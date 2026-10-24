package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

const secret48 = "0123456789abcdef0123456789abcdef0123456789abcdef"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func fullEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":           "postgres://u:p@localhost:5432/db",
		"STORAGE_SIGNING_SECRET": secret48,
		"EDIT_TOKEN_SECRET":      secret48,
		"OTP_PEPPER":             secret48,
		"ADMIN_TOKEN_SECRET":     secret48,
		"WA_NOTIF_TOKEN":         "tok",
		"TURNSTILE_SECRET":       "ts",
	}
}

// minimal: konfigurasi terkecil yang valid untuk mode none.
const minimalNone = `{
  "app": {"public_base_url": "https://api.example.com", "allowed_origins": ["https://app.example.com"]},
  "auth": {"mode": "none"},
  "storage": {"driver": "local", "local": {"root": "/data/photos"}}
}`

func TestParseMinimalNone(t *testing.T) {
	cfg, err := Parse([]byte(minimalNone), env(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.HTTPAddr != ":8080" || cfg.Edit.Window.Std() != 24*time.Hour || cfg.Retention.Days != 90 {
		t.Fatalf("default tidak terterapkan: %+v", cfg.App)
	}
	if cfg.Retention.OrphanSweepInterval.Std() != 24*time.Hour || cfg.Retention.TmpMaxAge.Std() != time.Hour ||
		cfg.Retention.OrphanMinAge.Std() != 24*time.Hour {
		t.Fatalf("default retention: %+v", cfg.Retention)
	}
	if cfg.Limits.MaxUploadBytes != 2<<20 || cfg.Storage.Local.MinFreeBytes != 5<<30 || !cfg.Storage.Local.RequireMarker {
		t.Fatalf("default storage/limits salah: %+v %+v", cfg.Storage, cfg.Limits)
	}
	if cfg.Secrets.DatabaseURL == "" || cfg.Secrets.EditTokenSecret != secret48 {
		t.Fatal("secret dari env tidak terbaca")
	}
}

func TestParseOverridesDefaults(t *testing.T) {
	raw := strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "edit": {"window": "1h"}, "retention": {"days": 30}`, 1)
	cfg, err := Parse([]byte(raw), env(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Edit.Window.Std() != time.Hour || cfg.Retention.Days != 30 || cfg.Retention.CleanupBatch != 100 {
		t.Fatalf("override/merge salah: %+v %+v", cfg.Edit, cfg.Retention)
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	raw, err := os.ReadFile("../../config.json.example")
	if err != nil {
		t.Fatal(err)
	}
	// nomor superadmin di contoh sengaja placeholder yang ditolak validasi
	if _, err := Parse(raw, env(fullEnv())); err == nil || !strings.Contains(err.Error(), "admin.superadmin.phone") {
		t.Fatalf("placeholder nomor superadmin harus ditolak: %v", err)
	}
	raw = []byte(strings.Replace(string(raw), "GANTI-dengan-nomor-HP-superadmin", "0812-3456-7890", 1))
	cfg, err := Parse(raw, env(fullEnv()))
	if err != nil {
		t.Fatalf("config.json.example (setelah nomor superadmin diisi) harus valid: %v", err)
	}
	if cfg.Auth.Mode != AuthModeOTP || !cfg.Admin.Enabled || cfg.Admin.Superadmin.Phone != "6281234567890" {
		t.Fatalf("contoh seharusnya mode otp + admin dengan nomor kanonik: %+v %+v", cfg.Auth, cfg.Admin.Superadmin)
	}
	// tanpa env penimpa, placeholder "GANTI" di contoh harus ditolak (tidak boleh terpakai di produksi)
	_, err = Parse(raw, env(map[string]string{}))
	for _, f := range []string{"secrets.database_url", "secrets.otp_pepper", "secrets.wa_notif_token", "secrets.turnstile_secret"} {
		if err == nil || !strings.Contains(err.Error(), f) || !strings.Contains(err.Error(), "placeholder") {
			t.Fatalf("placeholder %s harus ditolak: %v", f, err)
		}
	}
}

func TestParseFailures(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		mutEnv  func(map[string]string)
		wantErr []string // semua substring harus ada
	}{
		{"auth.mode kosong", `{"app": {"public_base_url": "https://a.example", "allowed_origins": ["https://b.example"]}, "storage": {"driver": "local", "local": {"root": "/x"}}}`, nil, []string{"auth.mode: wajib diisi"}},
		{"auth.mode tak dikenal", strings.Replace(minimalNone, `"none"`, `"jwt"`, 1), nil, []string{`auth.mode: "jwt" tidak dikenal`}},
		{"none tanpa EDIT_TOKEN_SECRET", minimalNone, func(m map[string]string) { delete(m, "EDIT_TOKEN_SECRET") }, []string{"EDIT_TOKEN_SECRET"}},
		{"otp tanpa OTP_PEPPER", strings.Replace(minimalNone, `"none"`, `"otp"`, 1), func(m map[string]string) { delete(m, "OTP_PEPPER") }, []string{"OTP_PEPPER", "wa_notif.base_url"}},
		{"otp tanpa WA_NOTIF_TOKEN", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "otp"}, "wa_notif": {"base_url": "http://wa:3030"}`, 1), func(m map[string]string) { delete(m, "WA_NOTIF_TOKEN") }, []string{"WA_NOTIF_TOKEN"}},
		{"secret terlalu pendek", minimalNone, func(m map[string]string) { m["STORAGE_SIGNING_SECRET"] = "short" }, []string{"STORAGE_SIGNING_SECRET): terlalu pendek"}},
		{"DATABASE_URL kosong", minimalNone, func(m map[string]string) { delete(m, "DATABASE_URL") }, []string{"DATABASE_URL"}},
		{"field tak dikenal (secret di file)", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none", "otp_pepper": "x"}`, 1), nil, []string{"unknown field"}},
		{"durasi bukan string", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "edit": {"window": 24}`, 1), nil, []string{"durasi harus berupa string"}},
		{"durasi tidak valid", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "edit": {"window": "dua hari"}`, 1), nil, []string{`durasi "dua hari" tidak valid`}},
		{"root relatif", strings.Replace(minimalNone, `/data/photos`, `data/photos`, 1), nil, []string{"storage.local.root: harus path absolut"}},
		{"driver s3 belum didukung", strings.Replace(minimalNone, `"driver": "local"`, `"driver": "s3"`, 1), nil, []string{`storage.driver: "s3" belum didukung`}},
		{"trusted proxy tidak valid", strings.Replace(minimalNone, `"allowed_origins": ["https://app.example.com"]`, `"allowed_origins": ["https://app.example.com"], "trusted_proxies": ["bukan-ip"]`, 1), nil, []string{"app.trusted_proxies[0]"}},
		{"origin berpath", strings.Replace(minimalNone, `https://app.example.com`, `https://app.example.com/app`, 1), nil, []string{"allowed_origins[0]"}},
		{"public_base_url diakhiri slash", strings.Replace(minimalNone, `https://api.example.com`, `https://api.example.com/`, 1), nil, []string{"app.public_base_url"}},
		{"admin field lama username/password", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "admin": {"enabled": false, "username": "a", "password_bcrypt": "$2a$10$FCu4N2Hbf06J0yoSrrEd5ukjWw1w/U3I4RxiGQwtHuzwOAm/1IXmy"}`, 1), nil, []string{"login admin kini lewat OTP", "admin.superadmin.phone"}},
		{"admin tanpa nomor superadmin", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "admin": {"enabled": true, "superadmin": {"name": "Admin"}}, "wa_notif": {"base_url": "http://wa:3030"}`, 1), nil, []string{"admin.superadmin.phone"}},
		{"admin nomor bukan seluler Indonesia", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "admin": {"enabled": true, "superadmin": {"name": "Admin", "phone": "+1 555 0100"}}, "wa_notif": {"base_url": "http://wa:3030"}`, 1), nil, []string{"admin.superadmin.phone"}},
		{"admin nama kosong", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "admin": {"enabled": true, "superadmin": {"name": " ", "phone": "081234567890"}}, "wa_notif": {"base_url": "http://wa:3030"}`, 1), nil, []string{"admin.superadmin.name"}},
		{"admin template tanpa {code}", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "admin": {"enabled": true, "superadmin": {"name": "Admin", "phone": "081234567890"}, "otp_message_template": "kode admin"}, "wa_notif": {"base_url": "http://wa:3030"}`, 1), nil, []string{"admin.otp_message_template"}},
		{"admin tanpa ADMIN_TOKEN_SECRET", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "admin": {"enabled": true, "superadmin": {"name": "Admin", "phone": "081234567890"}}, "wa_notif": {"base_url": "http://wa:3030"}`, 1), func(m map[string]string) { delete(m, "ADMIN_TOKEN_SECRET") }, []string{"ADMIN_TOKEN_SECRET"}},
		// login admin lewat OTP di mode none tetap butuh wa-notif dan pepper
		{"admin mode none tanpa wa-notif & pepper", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "admin": {"enabled": true, "superadmin": {"name": "Admin", "phone": "081234567890"}}`, 1), func(m map[string]string) { delete(m, "OTP_PEPPER"); delete(m, "WA_NOTIF_TOKEN") }, []string{"wa_notif.base_url", "OTP_PEPPER", "WA_NOTIF_TOKEN", "admin.enabled"}},
		{"turnstile tanpa secret", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "turnstile": {"enabled": true}`, 1), func(m map[string]string) { delete(m, "TURNSTILE_SECRET") }, []string{"TURNSTILE_SECRET"}},
		{"teks anotasi melebihi batas DB", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "limits": {"max_annotation_text_len": 1001}`, 1), nil, []string{"limits.max_annotation_text_len"}},
		{"orphan_min_age lebih pendek dari masa berlaku URL upload", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "retention": {"orphan_min_age": "1m"}`, 1), nil, []string{"retention.orphan_min_age"}},
		{"orphan_sweep_interval nol", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "retention": {"orphan_sweep_interval": "0s"}`, 1), nil, []string{"orphan_sweep_interval"}},
		{"tmp_max_age nol", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "retention": {"tmp_max_age": "0s"}`, 1), nil, []string{"tmp_max_age"}},
		{"wa_notif.send_interval nol", strings.Replace(strings.Replace(minimalNone, `"none"`, `"otp"`, 1), `"storage"`, `"wa_notif": {"base_url": "http://wa:3030", "send_interval": "0s"}, "storage"`, 1), nil, []string{"wa_notif.send_interval"}},
		{"limit nol", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "limits": {"max_concurrent_uploads": 0}`, 1), nil, []string{"limits"}},
		{"max_conns di luar rentang", strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"}, "database": {"max_conns": 500}`, 1), nil, []string{"database.max_conns"}},
		{"JSON rusak", `{`, nil, []string{"config:"}},
		{"data setelah objek", minimalNone + `{}`, nil, []string{"config:"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := fullEnv()
			if tc.mutEnv != nil {
				tc.mutEnv(e)
			}
			_, err := Parse([]byte(tc.raw), env(e))
			if err == nil {
				t.Fatal("seharusnya gagal")
			}
			for _, w := range tc.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("pesan error %q tidak memuat %q", err.Error(), w)
				}
			}
		})
	}
}

func TestAllProblemsReportedAtOnce(t *testing.T) {
	_, err := Parse([]byte(`{"app": {"public_base_url": "ftp://x"}, "auth": {"mode": "none"}, "storage": {"driver": "local", "local": {"root": "rel"}}}`), env(map[string]string{}))
	if err == nil {
		t.Fatal("seharusnya gagal")
	}
	for _, w := range []string{"app.public_base_url", "app.allowed_origins", "EDIT_TOKEN_SECRET", "DATABASE_URL", "STORAGE_SIGNING_SECRET", "storage.local.root"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error gabungan tidak memuat %q:\n%v", w, err)
		}
	}
}

func TestSecretsAreRedacted(t *testing.T) {
	cfg, err := Parse([]byte(minimalNone), env(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"%v":  fmt.Sprintf("%v", cfg.Secrets),
		"%+v": fmt.Sprintf("%+v", cfg.Secrets),
		"%#v": fmt.Sprintf("%#v", cfg.Secrets),
		"%s":  cfg.Secrets.String(),
	} {
		if strings.Contains(got, secret48) || strings.Contains(got, "postgres://") {
			t.Errorf("%s membocorkan secret: %s", name, got)
		}
	}
	if v := cfg.Secrets.LogValue(); v.Kind() != slog.KindString || strings.Contains(v.String(), secret48) {
		t.Errorf("LogValue membocorkan secret: %v", v)
	}
}

func TestLoadAndPath(t *testing.T) {
	if got := Path(env(map[string]string{})); got != "config.json" {
		t.Fatalf("default path = %q", got)
	}
	if got := Path(env(map[string]string{"CONFIG_PATH": " /etc/pa/config.json "})); got != "/etc/pa/config.json" {
		t.Fatalf("CONFIG_PATH path = %q", got)
	}
	if _, err := Load("/tidak/ada.json", env(fullEnv())); err == nil || !strings.Contains(err.Error(), "membaca") {
		t.Fatalf("file tak ada harus gagal: %v", err)
	}
	p := t.TempDir() + "/c.json"
	if err := os.WriteFile(p, []byte(minimalNone), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p, env(fullEnv())); err != nil {
		t.Fatal(err)
	}
}

func TestParseProxy(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1":       "127.0.0.1/32",
		"::1":             "::1/128",
		"172.16.5.9/12":   "172.16.0.0/12",
		"::ffff:10.0.0.1": "10.0.0.1/32",
	} {
		p, err := ParseProxy(in)
		if err != nil || p.String() != want {
			t.Errorf("ParseProxy(%q) = %v, %v; want %s", in, p, err, want)
		}
	}
	for _, in := range []string{"", "abc", "1.2.3.4/99", "1.2.3"} {
		if _, err := ParseProxy(in); err == nil {
			t.Errorf("ParseProxy(%q) seharusnya gagal", in)
		}
	}
}

func TestSecretsFromFileAndEnvOverride(t *testing.T) {
	raw := strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"},
  "secrets": {"database_url": "postgres://file@db/x", "storage_signing_secret": " `+secret48+` ", "edit_token_secret": "`+secret48+`"}`, 1)
	cfg, err := Parse([]byte(raw), env(map[string]string{}))
	if err != nil {
		t.Fatalf("secret dari config.json saja harus cukup: %v", err)
	}
	if cfg.Secrets.DatabaseURL != "postgres://file@db/x" || cfg.Secrets.StorageSigningSecret != secret48 {
		t.Fatalf("secret file tidak terbaca/di-trim: %q", cfg.Secrets.DatabaseURL)
	}
	cfg, err = Parse([]byte(raw), env(map[string]string{"DATABASE_URL": "postgres://env@db/x"}))
	if err != nil || cfg.Secrets.DatabaseURL != "postgres://env@db/x" {
		t.Fatalf("env harus menimpa file: %v", err)
	}
	b, _ := json.Marshal(cfg)
	if strings.Contains(string(b), secret48) || strings.Contains(string(b), "postgres://") {
		t.Fatalf("marshal config membocorkan secret: %s", b)
	}
}

func TestLoadRejectsWorldReadableConfig(t *testing.T) {
	p := t.TempDir() + "/c.json"
	if err := os.WriteFile(p, []byte(minimalNone), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p, env(fullEnv())); err == nil || !strings.Contains(err.Error(), "chmod 640") {
		t.Fatalf("config o+r harus ditolak: %v", err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p, env(fullEnv())); err != nil {
		t.Fatalf("mode 640 harus diterima: %v", err)
	}
}

func TestParseAdminSuperadmin(t *testing.T) {
	raw := strings.Replace(minimalNone, `"auth": {"mode": "none"}`, `"auth": {"mode": "none"},
  "admin": {"enabled": true, "superadmin": {"name": "  Super Admin ", "phone": "+62 0812-3456-7890"}},
  "wa_notif": {"base_url": "http://wa:3030"}`, 1)
	cfg, err := Parse([]byte(raw), env(fullEnv()))
	if err != nil {
		t.Fatalf("admin pada mode none harus diterima: %v", err)
	}
	a := cfg.Admin
	if a.Superadmin.Phone != "6281234567890" || a.Superadmin.Name != "Super Admin" || a.TokenTTL.Std() != 8*time.Hour ||
		!strings.Contains(a.OTPMessageTemplate, "ADMIN") {
		t.Fatalf("superadmin tidak dinormalisasi/default salah: %+v", a)
	}
}
