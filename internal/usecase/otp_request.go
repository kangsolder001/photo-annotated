package usecase

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// OTPRequestConfig adalah parameter permintaan OTP (spec §9.5).
type OTPRequestConfig struct {
	ResendCooldown time.Duration // ditampilkan sebagai resend_after_seconds
	MaxQueue       int           // challenge queued maksimal; penuh → 503 OTP_BUSY
	DailyGlobalCap int           // challenge yang dibuat per 24 jam; terlampaui → 503 OTP_BUSY
	SendInterval   time.Duration // jeda kirim wa-notif, untuk estimated_wait
	CodeTTL        time.Duration // masa berlaku kode sejak terkirim (untuk status challenge palsu)
	// OnQueued dipanggil setelah challenge baru diantrekan (membangunkan dispatcher); opsional.
	OnQueued func()
}

// RegisterInput adalah isi POST /auth/register.
type RegisterInput struct {
	Name, Email, Phone, Password string
	ClientIP                     *netip.Addr
}

// OTPTicket adalah respons 202 yang bentuknya sama untuk nomor terdaftar maupun tidak.
type OTPTicket struct {
	ChallengeID   uuid.UUID
	ResendAfter   time.Duration
	EstimatedWait time.Duration
}

// otpCore adalah antrean OTP bersama: OTP pengguna (registrasi/reset) dan login superadmin memakai
// repo, kode turunan, kanal, batas per nomor, max_queue, dan daily_global_cap yang sama (tidak ada
// antrean kedua). Catatan challenge palsu (decoys) terpisah per jenis, karena challenge asli jenis
// berbeda pada nomor yang sama juga tidak saling membatalkan.
type otpCore struct {
	otps    OTPRepo
	codes   OTPCodes
	channel *Channel
	phones  *PhoneLimiter
	clock   Clock
	cfg     OTPRequestConfig
	decoys  *decoys
}

func newOTPCore(otps OTPRepo, codes OTPCodes, channel *Channel, phones *PhoneLimiter, clock Clock, cfg OTPRequestConfig) *otpCore {
	if cfg.MaxQueue < 1 || cfg.DailyGlobalCap < 1 || cfg.SendInterval <= 0 || cfg.ResendCooldown < 0 || cfg.CodeTTL <= 0 {
		panic("usecase: OTPRequestConfig tidak valid")
	}
	return &otpCore{otps: otps, codes: codes, channel: channel, phones: phones, clock: clock, cfg: cfg,
		decoys: newDecoys(maxDecoys, cfg.CodeTTL)}
}

// OTPRequest membuat challenge OTP. OTP hanya dipakai untuk membuktikan kepemilikan nomor saat
// registrasi dan saat reset password; login biasa memakai nomor + password (PasswordLogin).
// Pengiriman tidak dilakukan di sini: challenge disimpan queued dan dikirim dispatcher (spec §4.0).
type OTPRequest struct {
	*otpCore
	users  UserRepo
	hasher PasswordHasher
}

// maxDecoys membatasi memori untuk challenge palsu (± 100 byte per entri).
const maxDecoys = 10_000

func NewOTPRequest(users UserRepo, otps OTPRepo, codes OTPCodes, hasher PasswordHasher, channel *Channel, phones *PhoneLimiter, clock Clock, cfg OTPRequestConfig) *OTPRequest {
	return &OTPRequest{otpCore: newOTPCore(otps, codes, channel, phones, clock, cfg), users: users, hasher: hasher}
}

var (
	errOTPUnavailable = domain.Unavailable("OTP_UNAVAILABLE", "WhatsApp delivery is having problems right now; try again later")
	errOTPBusy        = domain.Unavailable("OTP_BUSY", "too many verification requests right now; try again in a minute")
)

// Register memvalidasi data lalu mengirim kode untuk memverifikasi nomor. Data registrasi (nama,
// email, hash password) disimpan di challenge dan baru diterapkan saat kode challenge ini
// diverifikasi. Nomor yang sudah aktif tidak bisa didaftarkan ulang: responsnya sama (anti-enumerasi)
// tetapi tidak ada yang dikirim; pemiliknya memakai login atau reset password.
func (o *OTPRequest) Register(ctx context.Context, in RegisterInput) (*OTPTicket, error) {
	name, err := domain.NormalizeName(in.Name)
	if err != nil {
		return nil, err
	}
	email, err := domain.NormalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	phone, err := domain.NormalizePhone(in.Phone)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	ahead, err := o.admit(ctx, phone)
	if err != nil {
		return nil, err
	}
	now := o.clock.Now()
	u, _, err := o.users.FindOrCreatePending(ctx, &domain.User{
		ID: uuid.New(), Name: name, Email: email, Phone: phone, Status: domain.UserPending, CreatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	if u.Status != domain.UserPending {
		return o.decoy(phone, domain.OTPPurposeRegister, ahead), nil
	}
	// hash setelah admit: hanya permintaan yang lolos batas yang memakan CPU bcrypt
	hash, err := o.hasher.Hash(in.Password)
	if err != nil {
		return nil, err
	}
	return o.issue(ctx, &domain.OTPChallenge{
		UserID: u.ID, Phone: phone, Purpose: domain.OTPPurposeRegister,
		RegName: name, RegEmail: email, RegPasswordHash: hash,
	}, in.ClientIP, ahead, now)
}

// Forgot mengirim kode reset password ke nomor yang sudah aktif. Untuk nomor lain tidak ada yang
// dikirim, tetapi respons dan pembatasannya sama (anti-enumerasi).
func (o *OTPRequest) Forgot(ctx context.Context, rawPhone string, clientIP *netip.Addr) (*OTPTicket, error) {
	phone, err := domain.NormalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}
	ahead, err := o.admit(ctx, phone)
	if err != nil {
		return nil, err
	}
	u, err := o.users.GetByPhone(ctx, phone)
	// akun diblokir tidak bisa mereset password untuk membuka blokir: challenge palsu, tanpa pesan
	if errors.Is(err, domain.ErrNotFound) || (err == nil && (u.Status != domain.UserActive || u.Blocked())) {
		return o.decoy(phone, domain.OTPPurposeReset, ahead), nil
	}
	if err != nil {
		return nil, err
	}
	return o.issue(ctx, &domain.OTPChallenge{UserID: u.ID, Phone: phone, Purpose: domain.OTPPurposeReset}, clientIP, ahead, o.clock.Now())
}

// Resend mengirim kode baru untuk challenge yang sama tujuannya (data registrasi ikut disalin),
// sehingga FE tidak perlu menyimpan password untuk kirim ulang. Challenge lama menjadi expired.
func (o *OTPRequest) Resend(ctx context.Context, challengeID uuid.UUID, clientIP *netip.Addr) (*OTPTicket, error) {
	c, err := o.otps.Get(ctx, challengeID)
	if err == nil && c.Purpose == domain.OTPPurposeAdminLogin {
		err = domain.ErrNotFound // challenge admin hanya dikenal lewat /admin/otp/*
	}
	if errors.Is(err, domain.ErrNotFound) {
		d, ok := o.decoys.get(challengeID)
		if !ok {
			return nil, errOTPExpired // id tak dikenal / sudah dibersihkan: mulai dari awal
		}
		ahead, err := o.admit(ctx, d.phone)
		if err != nil {
			return nil, err
		}
		return o.decoy(d.phone, d.purpose, ahead), nil
	}
	if err != nil {
		return nil, err
	}
	if c.Status == domain.OTPConsumed {
		return nil, errOTPExpired
	}
	ahead, err := o.admit(ctx, c.Phone)
	if err != nil {
		return nil, err
	}
	switch c.Purpose {
	case domain.OTPPurposeRegister:
		// sudah diselesaikan lewat challenge lain: tidak ada yang perlu dikirim lagi
		if u, err := o.users.Get(ctx, c.UserID); err != nil || u.Status != domain.UserPending {
			return o.decoy(c.Phone, c.Purpose, ahead), nil
		}
	case domain.OTPPurposeReset:
		if u, err := o.users.Get(ctx, c.UserID); err != nil || u.Blocked() {
			return o.decoy(c.Phone, c.Purpose, ahead), nil
		}
	}
	return o.issue(ctx, &domain.OTPChallenge{
		UserID: c.UserID, Phone: c.Phone, Purpose: c.Purpose,
		RegName: c.RegName, RegEmail: c.RegEmail, RegPasswordHash: c.RegPasswordHash,
	}, clientIP, ahead, o.clock.Now())
}

// admit menjalankan pemeriksaan yang sama untuk semua nomor: kanal, kapasitas global, lalu batas per
// nomor (terakhir, agar penolakan karena kapasitas tidak menghabiskan jatah nomor itu). Mengembalikan
// jumlah challenge yang sedang mengantre.
func (o *otpCore) admit(ctx context.Context, phone string) (int, error) {
	if !o.channel.Available() {
		return 0, errOTPUnavailable
	}
	queued, err := o.otps.CountQueued(ctx)
	if err != nil {
		return 0, err
	}
	if queued >= o.cfg.MaxQueue {
		return 0, errOTPBusy.WithRetryAfter(time.Duration(queued) * o.cfg.SendInterval)
	}
	daily, err := o.otps.CountCreatedSince(ctx, o.clock.Now().Add(-24*time.Hour))
	if err != nil {
		return 0, err
	}
	if daily >= o.cfg.DailyGlobalCap {
		return 0, errOTPBusy.WithRetryAfter(time.Hour)
	}
	if ok, wait := o.phones.Allow(phone); !ok {
		return 0, domain.RateLimited("RATE_LIMITED", "too many code requests for this number; wait before retrying").WithRetryAfter(wait)
	}
	return queued, nil
}

// issue menyimpan challenge baru dari template (pengguna, tujuan, data registrasi); challenge aktif
// lama milik nomor itu menjadi expired. Bila gagal disimpan, jatah nomor dikembalikan karena tidak
// ada kode yang akan dikirim.
func (o *otpCore) issue(ctx context.Context, tmpl *domain.OTPChallenge, clientIP *netip.Addr, ahead int, now time.Time) (*OTPTicket, error) {
	id := uuid.New()
	c := *tmpl
	c.ID, c.CodeHash = id, o.codes.Hash(id, o.codes.Code(id))
	c.Status, c.NextAttemptAt, c.ClientIP, c.CreatedAt = domain.OTPQueued, now, clientIP, now
	if err := o.otps.Replace(ctx, &c); err != nil {
		o.phones.Refund(c.Phone)
		return nil, err
	}
	if o.cfg.OnQueued != nil {
		o.cfg.OnQueued()
	}
	return o.ticket(id, ahead), nil
}

// decoy membagikan challenge palsu yang statusnya berjalan seperti yang asli (lihat decoys).
func (o *otpCore) decoy(phone string, purpose domain.OTPPurpose, ahead int) *OTPTicket {
	t := o.ticket(uuid.New(), ahead)
	now := o.clock.Now()
	o.decoys.add(t.ChallengeID, phone, purpose, now, now.Add(t.EstimatedWait))
	return t
}

func (o *otpCore) ticket(id uuid.UUID, ahead int) *OTPTicket {
	// challenge ini menunggu yang di depannya ditambah gilirannya sendiri
	return &OTPTicket{ChallengeID: id, ResendAfter: o.cfg.ResendCooldown, EstimatedWait: time.Duration(ahead+1) * o.cfg.SendInterval}
}
