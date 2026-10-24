package usecase

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/ratelimit"
)

// PasswordLoginConfig: masa berlaku sesi dan batas percobaan login per nomor.
type PasswordLoginConfig struct {
	SessionTTL       time.Duration
	AttemptsPerPhone int           // percobaan login per nomor per Window (berhasil atau gagal)
	Window           time.Duration // mis. 15 menit
}

// PasswordLogin: masuk dengan nomor HP + password (tanpa OTP).
type PasswordLogin struct {
	users    UserRepo
	sessions SessionRepo
	hasher   PasswordHasher
	attempts *ratelimit.Limiter
	clock    Clock
	cfg      PasswordLoginConfig
}

func NewPasswordLogin(users UserRepo, sessions SessionRepo, hasher PasswordHasher, clock Clock, cfg PasswordLoginConfig) *PasswordLogin {
	if cfg.SessionTTL <= 0 || cfg.AttemptsPerPhone < 1 || cfg.Window <= 0 {
		panic("usecase: PasswordLoginConfig tidak valid")
	}
	return &PasswordLogin{users: users, sessions: sessions, hasher: hasher, clock: clock, cfg: cfg,
		attempts: ratelimit.NewWithOptions(cfg.AttemptsPerPhone, cfg.Window, 100_000, clock.Now)}
}

var (
	errInvalidLogin   = domain.Unauthorized("INVALID_CREDENTIALS", "phone number or password is incorrect")
	errAccountBlocked = domain.Forbidden("ACCOUNT_BLOCKED", "this account has been blocked; contact the admin")
)

// Login memeriksa nomor + password. Nomor tak terdaftar, belum diverifikasi, belum punya password,
// atau password salah menghasilkan pesan dan waktu respons yang sama (bcrypt selalu dijalankan).
// Percobaan dibatasi per nomor, sama untuk nomor terdaftar maupun tidak, agar password satu akun
// tidak bisa ditebak dari banyak IP.
func (l *PasswordLogin) Login(ctx context.Context, rawPhone, password string, clientIP *netip.Addr) (*LoginResult, error) {
	phone, err := domain.NormalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}
	if ok, wait := l.attempts.Allow(phone); !ok {
		return nil, domain.RateLimited("LOGIN_LOCKED", "too many sign-in attempts for this number; try again later").WithRetryAfter(wait)
	}
	if password == "" || len(password) > domain.MaxPasswordBytes {
		return nil, errInvalidLogin
	}
	u, err := l.users.GetByPhone(ctx, phone)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	hash := ""
	if err == nil && u.Status == domain.UserActive {
		hash = u.PasswordHash
	}
	if !l.hasher.Compare(hash, password) { // hash kosong: tetap menjalankan bcrypt tiruan
		return nil, errInvalidLogin
	}
	// diperiksa hanya setelah password terbukti benar, agar status blokir tidak bocor ke orang lain
	if u.Blocked() {
		return nil, errAccountBlocked
	}
	now := l.clock.Now()
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	s := &domain.Session{ID: uuid.New(), UserID: u.ID, TokenHash: tokenHash, CreatedAt: now, ExpiresAt: now.Add(l.cfg.SessionTTL), ClientIP: clientIP}
	if err := l.sessions.Create(ctx, s); err != nil {
		return nil, err
	}
	_ = l.users.TouchLogin(ctx, u.ID, now) // hanya catatan; kegagalannya tidak menggagalkan login
	return &LoginResult{Token: token, ExpiresAt: s.ExpiresAt, User: u}, nil
}
