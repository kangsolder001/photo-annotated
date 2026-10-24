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
}

// RegisterInput adalah isi POST /auth/register.
type RegisterInput struct {
	Name, Email, Phone string
	ClientIP           *netip.Addr
}

// OTPTicket adalah respons 202 yang bentuknya sama untuk nomor terdaftar maupun tidak.
type OTPTicket struct {
	ChallengeID   uuid.UUID
	ResendAfter   time.Duration
	EstimatedWait time.Duration
}

// OTPRequest membuat challenge OTP untuk registrasi dan login. Pengiriman tidak dilakukan di sini:
// challenge disimpan berstatus queued dan dikirim oleh dispatcher (spec §4.0).
type OTPRequest struct {
	users   UserRepo
	otps    OTPRepo
	codes   OTPCodes
	channel *Channel
	phones  *PhoneLimiter
	clock   Clock
	cfg     OTPRequestConfig
}

func NewOTPRequest(users UserRepo, otps OTPRepo, codes OTPCodes, channel *Channel, phones *PhoneLimiter, clock Clock, cfg OTPRequestConfig) *OTPRequest {
	if cfg.MaxQueue < 1 || cfg.DailyGlobalCap < 1 || cfg.SendInterval <= 0 || cfg.ResendCooldown < 0 {
		panic("usecase: OTPRequestConfig tidak valid")
	}
	return &OTPRequest{users: users, otps: otps, codes: codes, channel: channel, phones: phones, clock: clock, cfg: cfg}
}

var (
	errOTPUnavailable = domain.Unavailable("OTP_UNAVAILABLE", "WhatsApp delivery is having problems right now; try again later")
	errOTPBusy        = domain.Unavailable("OTP_BUSY", "too many verification requests right now; try again in a minute")
)

// Register memvalidasi data lalu membuat (atau memakai) pengguna dan challenge baru. Nomor yang sudah
// terdaftar diperlakukan sebagai login: nama/email miliknya tidak ditimpa.
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
	return o.issue(ctx, u, in.ClientIP, ahead, now)
}

// Request membuat challenge baru untuk login atau kirim ulang. Untuk nomor yang tidak terdaftar
// tidak ada yang dibuat atau dikirim, tetapi respons dan pembatasannya sama (anti-enumerasi).
func (o *OTPRequest) Request(ctx context.Context, rawPhone string, clientIP *netip.Addr) (*OTPTicket, error) {
	phone, err := domain.NormalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}
	ahead, err := o.admit(ctx, phone)
	if err != nil {
		return nil, err
	}
	u, err := o.users.GetByPhone(ctx, phone)
	if errors.Is(err, domain.ErrNotFound) {
		return o.ticket(uuid.New(), ahead), nil
	}
	if err != nil {
		return nil, err
	}
	return o.issue(ctx, u, clientIP, ahead, o.clock.Now())
}

// admit menjalankan pemeriksaan yang sama untuk semua nomor: kanal, kapasitas global, lalu batas per
// nomor (terakhir, agar penolakan karena kapasitas tidak menghabiskan jatah nomor itu). Mengembalikan
// jumlah challenge yang sedang mengantre.
func (o *OTPRequest) admit(ctx context.Context, phone string) (int, error) {
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

// issue menyimpan challenge baru (yang lama milik nomor itu menjadi expired). Bila gagal disimpan,
// jatah nomor dikembalikan karena tidak ada kode yang akan dikirim.
func (o *OTPRequest) issue(ctx context.Context, u *domain.User, clientIP *netip.Addr, ahead int, now time.Time) (*OTPTicket, error) {
	id := uuid.New()
	c := &domain.OTPChallenge{
		ID: id, UserID: u.ID, Phone: u.Phone, CodeHash: o.codes.Hash(id, o.codes.Code(id)),
		Status: domain.OTPQueued, NextAttemptAt: now, ClientIP: clientIP, CreatedAt: now,
	}
	if err := o.otps.Replace(ctx, c); err != nil {
		o.phones.Refund(u.Phone)
		return nil, err
	}
	return o.ticket(id, ahead), nil
}

func (o *OTPRequest) ticket(id uuid.UUID, ahead int) *OTPTicket {
	// challenge ini menunggu yang di depannya ditambah gilirannya sendiri
	return &OTPTicket{ChallengeID: id, ResendAfter: o.cfg.ResendCooldown, EstimatedWait: time.Duration(ahead+1) * o.cfg.SendInterval}
}
