package usecase

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// AdminLoginConfig adalah superadmin dari config.json dan parameter loginnya (spec-labels-admin §3).
type AdminLoginConfig struct {
	// OTP: parameter antrean OTP yang sama dengan OTP pengguna (OnQueued = dispatcher.Wake).
	OTP         OTPRequestConfig
	MaxAttempts int           // batas percobaan kode per challenge
	TokenTTL    time.Duration // masa berlaku token admin
	Name        string        // nama superadmin
	Phone       string        // nomor superadmin, bentuk kanonik 628…
}

// AdminProfile adalah identitas superadmin yang boleh ditampilkan (nomor disamarkan).
type AdminProfile struct {
	Name        string
	MaskedPhone string
}

// AdminSession dikembalikan saat login admin berhasil.
type AdminSession struct {
	Token     string
	ExpiresAt time.Time
	Admin     AdminProfile
}

// AdminLogin: login superadmin dengan OTP WhatsApp ke nomor di config, dan verifikasi token admin.
// Memakai antrean, dispatcher, batas per nomor, dan kapasitas global yang sama dengan OTP pengguna.
// Nomor selain nomor superadmin mendapat challenge palsu yang perilakunya sama (anti-enumerasi),
// sehingga nomor superadmin tidak bisa ditebak dari respons.
type AdminLogin struct {
	core   *otpCore
	tokens AdminTokenSigner
	cfg    AdminLoginConfig
}

func NewAdminLogin(otps OTPRepo, codes OTPCodes, channel *Channel, phones *PhoneLimiter, tokens AdminTokenSigner, clock Clock, cfg AdminLoginConfig) *AdminLogin {
	if cfg.MaxAttempts < 1 || cfg.TokenTTL <= 0 || cfg.Name == "" || !domain.IsCanonicalPhone(cfg.Phone) {
		panic("usecase: AdminLoginConfig tidak valid")
	}
	return &AdminLogin{core: newOTPCore(otps, codes, channel, phones, clock, cfg.OTP), tokens: tokens, cfg: cfg}
}

// Profile mengembalikan identitas superadmin.
func (a *AdminLogin) Profile() AdminProfile {
	return AdminProfile{Name: a.cfg.Name, MaskedPhone: domain.MaskPhone(a.cfg.Phone)}
}

// AdminRequestResult menyertakan apakah nomor itu nomor superadmin. Superadmin hanya untuk log
// server; tidak boleh memengaruhi respons HTTP.
type AdminRequestResult struct {
	Ticket      *OTPTicket
	MaskedPhone string
	Superadmin  bool
}

// Request membuat challenge login admin untuk nomor superadmin, atau challenge palsu untuk nomor lain.
// Pemeriksaan (kanal, kapasitas, batas per nomor) sama untuk semua nomor. Nomor tidak valid → res nil.
func (a *AdminLogin) Request(ctx context.Context, rawPhone string, clientIP *netip.Addr) (*AdminRequestResult, error) {
	phone, err := domain.NormalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}
	res := &AdminRequestResult{MaskedPhone: domain.MaskPhone(phone), Superadmin: phone == a.cfg.Phone}
	ahead, err := a.core.admit(ctx, phone)
	if err != nil {
		return res, err
	}
	if !res.Superadmin {
		res.Ticket = a.core.decoy(phone, domain.OTPPurposeAdminLogin, ahead)
		return res, nil
	}
	res.Ticket, err = a.core.issue(ctx, &domain.OTPChallenge{Phone: phone, Purpose: domain.OTPPurposeAdminLogin}, clientIP, ahead, a.core.clock.Now())
	return res, err
}

// adminChallenge mengambil challenge login admin; jenis lain diperlakukan seperti tidak ada.
func (a *AdminLogin) adminChallenge(ctx context.Context, id uuid.UUID) (*domain.OTPChallenge, error) {
	c, err := a.core.otps.Get(ctx, id)
	if err == nil && c.Purpose != domain.OTPPurposeAdminLogin {
		return nil, domain.ErrNotFound
	}
	return c, err
}

// Resend mengirim kode baru untuk challenge yang sama; challenge lama menjadi expired.
func (a *AdminLogin) Resend(ctx context.Context, challengeID uuid.UUID, clientIP *netip.Addr) (*OTPTicket, error) {
	c, err := a.adminChallenge(ctx, challengeID)
	if errors.Is(err, domain.ErrNotFound) {
		d, ok := a.core.decoys.get(challengeID)
		if !ok {
			return nil, errOTPExpired // id tak dikenal / sudah dibersihkan: mulai dari awal
		}
		ahead, err := a.core.admit(ctx, d.phone)
		if err != nil {
			return nil, err
		}
		return a.core.decoy(d.phone, d.purpose, ahead), nil
	}
	if err != nil {
		return nil, err
	}
	if c.Status == domain.OTPConsumed || c.Phone != a.cfg.Phone {
		return nil, errOTPExpired // sudah dipakai, atau nomor superadmin diganti di config
	}
	ahead, err := a.core.admit(ctx, c.Phone)
	if err != nil {
		return nil, err
	}
	return a.core.issue(ctx, &domain.OTPChallenge{Phone: c.Phone, Purpose: domain.OTPPurposeAdminLogin}, clientIP, ahead, a.core.clock.Now())
}

// Status sama dengan GET /auth/otp/{id}, termasuk perilaku id palsu.
func (a *AdminLogin) Status(ctx context.Context, id uuid.UUID) (*OTPStatusView, error) {
	return a.core.status(ctx, id, true)
}

// Verify memeriksa kode dan menerbitkan token admin. Nomor challenge dicocokkan ulang dengan nomor
// superadmin yang sedang berlaku: challenge yang dibuat sebelum nomor diganti → OTP_EXPIRED.
func (a *AdminLogin) Verify(ctx context.Context, challengeID uuid.UUID, code string) (*AdminSession, error) {
	if err := validCodeFormat(code); err != nil {
		return nil, err
	}
	now := a.core.clock.Now()
	c, err := a.adminChallenge(ctx, challengeID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, a.core.decoys.verify(challengeID, now, a.cfg.MaxAttempts)
	}
	if err != nil {
		return nil, err
	}
	if c.Phone != a.cfg.Phone {
		_ = a.core.otps.Expire(ctx, c.ID)
		return nil, errOTPExpired
	}
	if err := checkCode(ctx, a.core.otps, a.core.codes, c, code, a.cfg.MaxAttempts, now); err != nil {
		return nil, err
	}
	ok, err := a.core.otps.Consume(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errOTPExpired // sudah dipakai oleh verifikasi lain yang berbarengan
	}
	exp := now.Add(a.cfg.TokenTTL).Truncate(time.Second) // token menyimpan detik unix
	return &AdminSession{Token: a.tokens.Issue(exp), ExpiresAt: exp, Admin: a.Profile()}, nil
}

// VerifyToken mengembalikan waktu kedaluwarsa bila token adalah token admin yang sah.
func (a *AdminLogin) VerifyToken(token string) (time.Time, bool) {
	return a.tokens.Verify(token, a.core.clock.Now())
}
