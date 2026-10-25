// Package config memuat dan memvalidasi konfigurasi aplikasi.
//
// Konfigurasi non-rahasia dibaca dari config.json (field tak dikenal ditolak,
// sehingga secret tidak mungkin ikut tersimpan di file). Secret hanya dibaca dari
// environment. Semua masalah dilaporkan sekaligus dan membuat proses gagal start.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"photo-annotated/internal/domain"
)

const (
	AuthModeNone = "none"
	AuthModeOTP  = "otp"

	StorageDriverLocal = "local"

	minSecretLen = 32
)

// Duration adalah time.Duration yang di-JSON sebagai string ("10m", "720h").
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("durasi harus berupa string seperti \"10m\"")
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("durasi %q tidak valid", s)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

type Config struct {
	App       App       `json:"app"`
	Auth      Auth      `json:"auth"`
	WANotif   WANotif   `json:"wa_notif"`
	Admin     Admin     `json:"admin"`
	Edit      Edit      `json:"edit"`
	Turnstile Turnstile `json:"turnstile"`
	Database  Database  `json:"database"`
	Storage   Storage   `json:"storage"`
	Limits    Limits    `json:"limits"`
	Retention Retention `json:"retention"`

	// Secrets dibaca dari bagian "secrets" di config.json; env var dengan nama lama (DATABASE_URL,
	// OTP_PEPPER, …) bila diisi menimpanya (berguna untuk dev/test). Karena file memuat rahasia,
	// Load menolak config.json yang bisa dibaca semua pengguna.
	Secrets Secrets `json:"secrets"`
}

type App struct {
	Env            string   `json:"env"`
	HTTPAddr       string   `json:"http_addr"`
	PublicBaseURL  string   `json:"public_base_url"`
	AllowedOrigins []string `json:"allowed_origins"`
	// TrustedProxies: alamat/CIDR proxy (cloudflared) yang boleh menentukan IP klien lewat
	// header CF-Connecting-IP. Koneksi dari alamat lain tidak dipercaya header itu.
	TrustedProxies []string `json:"trusted_proxies"`
}

type Auth struct {
	Mode       string   `json:"mode"`
	SessionTTL Duration `json:"session_ttl"`
	OTP        OTP      `json:"otp"`
}

type OTP struct {
	Length             int      `json:"length"`
	TTL                Duration `json:"ttl"`
	MaxVerifyAttempts  int      `json:"max_verify_attempts"`
	ResendCooldown     Duration `json:"resend_cooldown"`
	MaxPerPhonePerHour int      `json:"max_per_phone_per_hour"`
	MaxQueue           int      `json:"max_queue"`
	DailyGlobalCap     int      `json:"daily_global_cap"`
	MessageTemplate    string   `json:"message_template"`
}

type WANotif struct {
	BaseURL             string   `json:"base_url"`
	Timeout             Duration `json:"timeout"`
	HealthProbeInterval Duration `json:"health_probe_interval"`
	// SendInterval: jeda minimum antar pesan di wa-notif (rate_limit_seconds miliknya, 1,5 dtk);
	// dipakai untuk memperkirakan lama tunggu OTP yang ditampilkan ke pengguna.
	SendInterval Duration `json:"send_interval"`
}

// Admin: satu superadmin yang login dengan OTP WhatsApp ke nomornya (spec-labels-admin §3.1).
type Admin struct {
	Enabled            bool       `json:"enabled"`
	Superadmin         Superadmin `json:"superadmin"`
	TokenTTL           Duration   `json:"token_ttl"`
	OTPMessageTemplate string     `json:"otp_message_template"`

	// Field lama login username/password. Tetap dikenali hanya agar bisa ditolak dengan pesan yang
	// jelas (bukan "unknown field"), sehingga salah konfigurasi tidak lolos diam-diam.
	Username       string `json:"username"`
	PasswordBcrypt string `json:"password_bcrypt"`
}

// Superadmin: nama dan nomor HP superadmin. Phone dinormalisasi ke bentuk kanonik 628… saat validasi.
type Superadmin struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type Edit struct {
	Window Duration `json:"window"`
}

type Turnstile struct {
	Enabled bool `json:"enabled"`
}

type Database struct {
	MaxConns int `json:"max_conns"`
}

type Storage struct {
	Driver       string       `json:"driver"`
	Local        StorageLocal `json:"local"`
	UploadURLTTL Duration     `json:"upload_url_ttl"`
	ViewURLTTL   Duration     `json:"view_url_ttl"`
}

type StorageLocal struct {
	Root          string `json:"root"`
	RequireMarker bool   `json:"require_marker"`
	MinFreeBytes  int64  `json:"min_free_bytes"`
}

type Limits struct {
	MaxUploadBytes         int64 `json:"max_upload_bytes"`
	MaxPhotosPerSubmission int   `json:"max_photos_per_submission"`
	MaxAnnotationsPerPhoto int   `json:"max_annotations_per_photo"`
	MaxLabels              int   `json:"max_labels"` // label aktif maksimal (spec-labels-admin §4.1)
	// MaxAnnotationTextLen dihapus (anotasi kini memakai label). Tetap dikenali hanya untuk ditolak
	// dengan pesan yang jelas.
	MaxAnnotationTextLen int        `json:"max_annotation_text_len"`
	MaxConcurrentUploads int        `json:"max_concurrent_uploads"`
	UploadTimeout        Duration   `json:"upload_timeout"`
	RateLimitPerMin      RateLimits `json:"rate_limit_per_min"`
}

type RateLimits struct {
	Presign     int `json:"presign"`
	Submissions int `json:"submissions"`
	OTPRequest  int `json:"otp_request"`
	OTPVerify   int `json:"otp_verify"`
	AdminLogin  int `json:"admin_login"`
	Login       int `json:"login"` // POST /auth/login per IP
}

type Retention struct {
	Days            int      `json:"days"`
	PendingPhotoTTL Duration `json:"pending_photo_ttl"`
	CleanupInterval Duration `json:"cleanup_interval"`
	CleanupBatch    int      `json:"cleanup_batch"`
	// OrphanSweepInterval: sapuan file yatim di disk (file tanpa baris foto), default harian.
	OrphanSweepInterval Duration `json:"orphan_sweep_interval"`
	// TmpMaxAge: sisa upload sementara (tmp/*.part) yang lebih tua dari ini dihapus.
	TmpMaxAge Duration `json:"tmp_max_age"`
	// OrphanMinAge: file tanpa baris foto baru dihapus bila lebih tua dari ini (melindungi
	// jendela singkat antara file terbit dan baris DB dicatat).
	OrphanMinAge Duration `json:"orphan_min_age"`
}

// Secrets berisi nilai rahasia. Nilainya disamarkan saat dicetak, di-log, atau di-marshal ulang.
type Secrets struct {
	DatabaseURL          string `json:"database_url"`
	StorageSigningSecret string `json:"storage_signing_secret"`
	EditTokenSecret      string `json:"edit_token_secret"`
	OTPPepper            string `json:"otp_pepper"`
	AdminTokenSecret     string `json:"admin_token_secret"`
	WANotifToken         string `json:"wa_notif_token"`
	TurnstileSecret      string `json:"turnstile_secret"`
}

// secretEnv memetakan env var penimpa ke field Secrets.
func (s *Secrets) secretEnv() map[string]*string {
	return map[string]*string{
		"DATABASE_URL": &s.DatabaseURL, "STORAGE_SIGNING_SECRET": &s.StorageSigningSecret,
		"EDIT_TOKEN_SECRET": &s.EditTokenSecret, "OTP_PEPPER": &s.OTPPepper, "ADMIN_TOKEN_SECRET": &s.AdminTokenSecret,
		"WA_NOTIF_TOKEN": &s.WANotifToken, "TURNSTILE_SECRET": &s.TurnstileSecret,
	}
}

func (Secrets) MarshalJSON() ([]byte, error) { return []byte(`"redacted"`), nil }
func (Secrets) String() string               { return "Secrets{redacted}" }
func (Secrets) GoString() string             { return "Secrets{redacted}" }
func (Secrets) LogValue() slog.Value {
	return slog.StringValue("redacted")
}

// Defaults mengembalikan nilai bawaan untuk parameter tuning. Field yang menentukan
// perilaku keamanan/lingkungan (auth.mode, storage.driver, root, URL publik, origin)
// sengaja tidak punya default dan wajib diisi.
func Defaults() Config {
	return Config{
		App: App{
			Env:            "production",
			HTTPAddr:       ":8080",
			TrustedProxies: []string{"127.0.0.1/32", "::1/128", "172.16.0.0/12"},
		},
		Auth: Auth{
			SessionTTL: Duration(30 * 24 * time.Hour),
			OTP: OTP{
				Length:             6,
				TTL:                Duration(5 * time.Minute),
				MaxVerifyAttempts:  5,
				ResendCooldown:     Duration(60 * time.Second),
				MaxPerPhonePerHour: 5,
				MaxQueue:           200,
				DailyGlobalCap:     500,
				MessageTemplate:    "*{code}* adalah kode verifikasi Photo Annotated Anda. Berlaku {ttl_minutes} menit. Jangan bagikan kode ini ke siapa pun.",
			},
		},
		WANotif: WANotif{
			Timeout:             Duration(10 * time.Second),
			HealthProbeInterval: Duration(30 * time.Second),
			SendInterval:        Duration(1500 * time.Millisecond),
		},
		Admin: Admin{
			TokenTTL:           Duration(8 * time.Hour),
			OTPMessageTemplate: "*{code}* adalah kode masuk ADMIN Photo Annotated. Berlaku {ttl_minutes} menit. Jika Anda tidak sedang masuk, abaikan dan periksa keamanan akun.",
		},
		Edit: Edit{Window: Duration(24 * time.Hour)},
		Turnstile: Turnstile{
			Enabled: false,
		},
		Database: Database{MaxConns: 20},
		Storage: Storage{
			Local:        StorageLocal{RequireMarker: true, MinFreeBytes: 5 << 30},
			UploadURLTTL: Duration(10 * time.Minute),
			ViewURLTTL:   Duration(5 * time.Minute),
		},
		Limits: Limits{
			MaxUploadBytes:         2 << 20,
			MaxPhotosPerSubmission: 5,
			MaxAnnotationsPerPhoto: 50,
			MaxLabels:              200,
			MaxConcurrentUploads:   200,
			UploadTimeout:          Duration(60 * time.Second),
			RateLimitPerMin:        RateLimits{Presign: 30, Submissions: 10, OTPRequest: 5, OTPVerify: 20, AdminLogin: 5, Login: 10},
		},
		Retention: Retention{
			Days:            90,
			PendingPhotoTTL: Duration(24 * time.Hour),
			CleanupInterval: Duration(time.Hour),
			CleanupBatch:    100,

			OrphanSweepInterval: Duration(24 * time.Hour),
			TmpMaxAge:           Duration(time.Hour),
			OrphanMinAge:        Duration(24 * time.Hour),
		},
	}
}

// Path mengembalikan lokasi config.json: env CONFIG_PATH, atau ./config.json.
func Path(getenv func(string) string) string {
	if p := strings.TrimSpace(getenv("CONFIG_PATH")); p != "" {
		return p
	}
	return "config.json"
}

// Load membaca file konfigurasi, menimpa secret dari environment bila ada, lalu memvalidasi.
// File yang bisa dibaca semua pengguna (o+r) ditolak karena memuat secret.
func Load(path string, getenv func(string) string) (*Config, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("config: membaca %s: %w", path, err)
	}
	if st.Mode().Perm()&0o007 != 0 {
		return nil, fmt.Errorf("config: %s memuat secret tetapi bisa diakses semua pengguna (mode %#o); jalankan chmod 640 (atau 600)", path, st.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: membaca %s: %w", path, err)
	}
	return Parse(raw, getenv)
}

// Parse sama seperti Load tetapi dari byte JSON (dipakai test).
func Parse(raw []byte, getenv func(string) string) (*Config, error) {
	cfg := Defaults()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if dec.More() {
		return nil, errors.New("config: ada data setelah objek JSON utama")
	}
	for name, field := range cfg.Secrets.secretEnv() {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			*field = v
		} else {
			*field = strings.TrimSpace(*field)
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate memeriksa seluruh konfigurasi dan melaporkan semua masalah sekaligus.
func (c *Config) Validate() error {
	var errs []error
	bad := func(field, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", field, fmt.Sprintf(format, a...)))
	}

	// app
	if strings.TrimSpace(c.App.HTTPAddr) == "" {
		bad("app.http_addr", "wajib diisi")
	}
	if err := checkAbsURL(c.App.PublicBaseURL, false); err != nil {
		bad("app.public_base_url", "%v", err)
	}
	if len(c.App.AllowedOrigins) == 0 {
		bad("app.allowed_origins", "minimal satu origin")
	}
	for i, o := range c.App.AllowedOrigins {
		if err := checkAbsURL(o, true); err != nil {
			bad(fmt.Sprintf("app.allowed_origins[%d]", i), "%v", err)
		}
	}
	for i, p := range c.App.TrustedProxies {
		if _, err := ParseProxy(p); err != nil {
			bad(fmt.Sprintf("app.trusted_proxies[%d]", i), "%v", err)
		}
	}

	// auth
	switch c.Auth.Mode {
	case AuthModeNone:
		requireSecret(&errs, "secrets.edit_token_secret (EDIT_TOKEN_SECRET)", c.Secrets.EditTokenSecret, "auth.mode=none")
	case AuthModeOTP:
		if c.Auth.SessionTTL <= 0 {
			bad("auth.session_ttl", "harus > 0")
		}
	case "":
		bad("auth.mode", "wajib diisi (%q atau %q)", AuthModeNone, AuthModeOTP)
	default:
		bad("auth.mode", "%q tidak dikenal (gunakan %q atau %q)", c.Auth.Mode, AuthModeNone, AuthModeOTP)
	}

	// OTP WhatsApp: dipakai auth.mode=otp dan login admin (di semua mode)
	if c.Auth.Mode == AuthModeOTP || c.Admin.Enabled {
		why := "auth.mode=otp"
		if c.Auth.Mode != AuthModeOTP {
			why = "admin.enabled (login admin lewat OTP)"
		}
		requireSecret(&errs, "secrets.otp_pepper (OTP_PEPPER)", c.Secrets.OTPPepper, why)
		if c.Secrets.WANotifToken == "" {
			bad("secrets.wa_notif_token (WA_NOTIF_TOKEN)", "wajib diisi jika %s", why)
		}
		if err := checkAbsURL(c.WANotif.BaseURL, false); err != nil {
			bad("wa_notif.base_url", "wajib untuk %s: %v", why, err)
		}
		if c.WANotif.Timeout <= 0 {
			bad("wa_notif.timeout", "harus > 0")
		}
		if c.WANotif.HealthProbeInterval <= 0 {
			bad("wa_notif.health_probe_interval", "harus > 0")
		}
		if c.WANotif.SendInterval <= 0 {
			bad("wa_notif.send_interval", "harus > 0")
		}
		o := c.Auth.OTP
		if o.Length < 4 || o.Length > 8 {
			bad("auth.otp.length", "harus 4-8")
		}
		if o.TTL <= 0 || o.ResendCooldown < 0 {
			bad("auth.otp", "ttl harus > 0 dan resend_cooldown >= 0")
		}
		if o.MaxVerifyAttempts < 1 || o.MaxPerPhonePerHour < 1 || o.MaxQueue < 1 || o.DailyGlobalCap < 1 {
			bad("auth.otp", "max_verify_attempts, max_per_phone_per_hour, max_queue, daily_global_cap harus >= 1")
		}
		if !strings.Contains(o.MessageTemplate, "{code}") {
			bad("auth.otp.message_template", "harus memuat {code}")
		}
	}

	// admin
	if c.Admin.Username != "" || c.Admin.PasswordBcrypt != "" {
		bad("admin.username/admin.password_bcrypt", "login admin kini lewat OTP; hapus field ini dan isi admin.superadmin.phone")
	}
	if c.Admin.Enabled {
		sa := &c.Admin.Superadmin
		if phone, err := domain.NormalizePhone(sa.Phone); err != nil {
			bad("admin.superadmin.phone", "harus nomor seluler Indonesia yang valid (nilai: %q)", sa.Phone)
		} else {
			sa.Phone = phone
		}
		if name, err := domain.NormalizeName(sa.Name); err != nil {
			bad("admin.superadmin.name", "wajib 1-100 karakter tanpa karakter kontrol")
		} else {
			sa.Name = name
		}
		if !strings.Contains(c.Admin.OTPMessageTemplate, "{code}") {
			bad("admin.otp_message_template", "harus memuat {code}")
		}
		requireSecret(&errs, "secrets.admin_token_secret (ADMIN_TOKEN_SECRET)", c.Secrets.AdminTokenSecret, "admin.enabled")
		if c.Admin.TokenTTL <= 0 {
			bad("admin.token_ttl", "harus > 0")
		}
	}

	// turnstile
	if c.Turnstile.Enabled && c.Secrets.TurnstileSecret == "" {
		bad("secrets.turnstile_secret (TURNSTILE_SECRET)", "wajib diisi jika turnstile.enabled")
	}

	// edit
	if c.Edit.Window <= 0 {
		bad("edit.window", "harus > 0")
	}

	// database
	if c.Secrets.DatabaseURL == "" {
		bad("secrets.database_url (DATABASE_URL)", "wajib diisi")
	}
	for name, v := range map[string]string{
		"secrets.database_url": c.Secrets.DatabaseURL, "secrets.wa_notif_token": c.Secrets.WANotifToken,
		"secrets.turnstile_secret": c.Secrets.TurnstileSecret,
	} {
		if strings.Contains(v, placeholder) {
			bad(name, "masih berisi placeholder dari config.json.example")
		}
	}
	if c.Database.MaxConns < 1 || c.Database.MaxConns > 100 {
		bad("database.max_conns", "harus 1-100")
	}

	// storage
	requireSecret(&errs, "secrets.storage_signing_secret (STORAGE_SIGNING_SECRET)", c.Secrets.StorageSigningSecret, "penyimpanan foto")
	switch c.Storage.Driver {
	case StorageDriverLocal:
		if !filepath.IsAbs(c.Storage.Local.Root) {
			bad("storage.local.root", "harus path absolut (nilai: %q)", c.Storage.Local.Root)
		}
		if c.Storage.Local.MinFreeBytes < 0 {
			bad("storage.local.min_free_bytes", "tidak boleh negatif")
		}
	case "":
		bad("storage.driver", "wajib diisi (%q)", StorageDriverLocal)
	default:
		bad("storage.driver", "%q belum didukung (hanya %q)", c.Storage.Driver, StorageDriverLocal)
	}
	if c.Storage.UploadURLTTL <= 0 || c.Storage.ViewURLTTL <= 0 {
		bad("storage", "upload_url_ttl dan view_url_ttl harus > 0")
	}

	// limits
	l := c.Limits
	if l.MaxUploadBytes < 1 {
		bad("limits.max_upload_bytes", "harus > 0")
	}
	if l.MaxPhotosPerSubmission < 1 || l.MaxAnnotationsPerPhoto < 1 || l.MaxConcurrentUploads < 1 || l.MaxLabels < 1 {
		bad("limits", "max_photos_per_submission, max_annotations_per_photo, max_concurrent_uploads, max_labels harus >= 1")
	}
	if l.MaxAnnotationTextLen != 0 {
		bad("limits.max_annotation_text_len", "dihapus: anotasi kini memakai label, bukan teks bebas; hapus field ini")
	}
	if l.UploadTimeout <= 0 {
		bad("limits.upload_timeout", "harus > 0")
	}
	r := l.RateLimitPerMin
	if r.Presign < 1 || r.Submissions < 1 || r.OTPRequest < 1 || r.OTPVerify < 1 || r.AdminLogin < 1 || r.Login < 1 {
		bad("limits.rate_limit_per_min", "semua nilai harus >= 1")
	}

	// retention
	t := c.Retention
	if t.Days < 1 || t.CleanupBatch < 1 || t.PendingPhotoTTL <= 0 || t.CleanupInterval <= 0 {
		bad("retention", "days >= 1, cleanup_batch >= 1, pending_photo_ttl dan cleanup_interval > 0")
	}
	if t.OrphanSweepInterval <= 0 || t.TmpMaxAge <= 0 || t.OrphanMinAge <= 0 {
		bad("retention", "orphan_sweep_interval, tmp_max_age, dan orphan_min_age harus > 0")
	}
	// file yang baru di-commit tidak boleh sempat dianggap yatim: harus lebih lama dari masa
	// berlaku URL upload (upload yang lambat) ditambah ruang aman
	if t.OrphanMinAge < c.Storage.UploadURLTTL {
		bad("retention.orphan_min_age", "tidak boleh lebih pendek dari storage.upload_url_ttl")
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("config tidak valid:\n  - %w", joinLines(errs))
}

// ParseProxy menerima alamat IP tunggal atau CIDR dan mengembalikannya sebagai prefix.
func ParseProxy(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("%q bukan CIDR yang valid", s)
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q bukan IP atau CIDR yang valid", s)
	}
	return netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()), nil
}

// placeholder menandai nilai contoh di config.json.example yang wajib diganti.
const placeholder = "GANTI"

func requireSecret(errs *[]error, name, val, why string) {
	switch {
	case strings.Contains(val, placeholder):
		*errs = append(*errs, fmt.Errorf("%s: masih berisi placeholder dari config.json.example", name))
	case val == "":
		*errs = append(*errs, fmt.Errorf("%s: wajib diisi untuk %s", name, why))
	case len(val) < minSecretLen:
		*errs = append(*errs, fmt.Errorf("%s: terlalu pendek (%d karakter, minimal %d)", name, len(val), minSecretLen))
	}
}

func checkAbsURL(s string, originOnly bool) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("wajib diisi")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q bukan URL http(s) absolut", s)
	}
	if originOnly && (u.Path != "" || u.RawQuery != "" || u.Fragment != "") {
		return fmt.Errorf("%q harus berupa origin (skema://host[:port]) tanpa path", s)
	}
	if !originOnly && strings.HasSuffix(s, "/") {
		return fmt.Errorf("%q tidak boleh diakhiri '/'", s)
	}
	return nil
}

// joinLines menggabungkan error menjadi satu per baris dengan indentasi.
func joinLines(errs []error) error {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return errors.New(strings.Join(msgs, "\n  - "))
}
