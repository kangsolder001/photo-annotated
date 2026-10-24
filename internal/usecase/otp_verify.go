package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// OTPVerifyConfig adalah parameter verifikasi dan sesi (spec §5.8, §9.5).
type OTPVerifyConfig struct {
	MaxAttempts int
	SessionTTL  time.Duration
}

// VerifyInput adalah isi POST /auth/otp/verify. NewPassword wajib untuk challenge reset password.
type VerifyInput struct {
	ChallengeID uuid.UUID
	Code        string
	NewPassword string
	ClientIP    *netip.Addr
}

// LoginResult dikembalikan saat kode benar. Token hanya ada di sini; yang disimpan hanya hash-nya.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      *domain.User
}

// OTPVerify memeriksa kode dan menerbitkan sesi.
type OTPVerify struct {
	users    UserRepo
	otps     OTPRepo
	sessions SessionRepo
	tx       TxManager
	codes    OTPCodes
	hasher   PasswordHasher
	decoys   *decoys
	clock    Clock
	cfg      OTPVerifyConfig
}

// NewOTPVerify memakai catatan challenge palsu milik req agar verifikasi id palsu berperilaku sama
// dengan challenge asli (anti-enumerasi).
func NewOTPVerify(req *OTPRequest, users UserRepo, otps OTPRepo, sessions SessionRepo, tx TxManager, codes OTPCodes, hasher PasswordHasher, clock Clock, cfg OTPVerifyConfig) *OTPVerify {
	if cfg.MaxAttempts < 1 || cfg.SessionTTL <= 0 {
		panic("usecase: OTPVerifyConfig tidak valid")
	}
	return &OTPVerify{users: users, otps: otps, sessions: sessions, tx: tx, codes: codes, hasher: hasher, decoys: req.decoys, clock: clock, cfg: cfg}
}

var errPasswordRequired = domain.Invalid("PASSWORD_REQUIRED", "a new password is required to reset the password")

var (
	errOTPNotSent  = domain.Conflict("OTP_NOT_SENT", "the code has not been sent yet; wait for it or request a new one")
	errOTPExpired  = domain.Gone("OTP_EXPIRED", "the code has expired; request a new one")
	errOTPTooMany  = domain.RateLimited("OTP_TOO_MANY_ATTEMPTS", "too many wrong codes; request a new one")
	errOTPBadInput = domain.Invalid("INVALID_CODE", "code must contain digits only")
)

func errOTPInvalid(left int) error {
	return domain.Invalid("OTP_INVALID", "the code is not correct").WithDetails(map[string]any{"attempts_left": left})
}

// Verify memeriksa kode. Percobaan dihitung secara atomik sebelum dibandingkan, sehingga tebakan
// paralel tidak dapat melewati batas; kode hanya sah sekali walau diverifikasi berbarengan.
// Registrasi: data dari challenge diterapkan dan akun aktif. Reset: password diganti dan semua sesi
// lama dicabut. Keduanya langsung login.
func (v *OTPVerify) Verify(ctx context.Context, in VerifyInput) (*LoginResult, error) {
	if len(in.Code) == 0 || len(in.Code) > 16 {
		return nil, errOTPBadInput
	}
	for _, r := range in.Code {
		if r < '0' || r > '9' {
			return nil, errOTPBadInput
		}
	}
	if in.NewPassword != "" {
		if err := domain.ValidatePassword(in.NewPassword); err != nil {
			return nil, err
		}
	}
	now := v.clock.Now()
	c, err := v.otps.Get(ctx, in.ChallengeID)
	if errors.Is(err, domain.ErrNotFound) {
		if d, ok := v.decoys.get(in.ChallengeID); ok && d.purpose == domain.OTPPurposeReset && in.NewPassword == "" {
			return nil, errPasswordRequired
		}
		return nil, v.verifyDecoy(in.ChallengeID, now)
	}
	if err != nil {
		return nil, err
	}
	// diperiksa sebelum percobaan dihitung: lupa mengisi password baru bukan tebakan kode
	if c.Purpose == domain.OTPPurposeReset && in.NewPassword == "" {
		return nil, errPasswordRequired
	}
	switch c.Status {
	case domain.OTPQueued, domain.OTPSending, domain.OTPFailed:
		return nil, errOTPNotSent
	case domain.OTPSent:
	default: // expired, consumed
		return nil, errOTPExpired
	}
	if c.ExpiresAt == nil || !now.Before(*c.ExpiresAt) {
		_ = v.otps.Expire(ctx, c.ID)
		return nil, errOTPExpired
	}
	n, err := v.otps.IncrementVerifyAttempts(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if n > v.cfg.MaxAttempts {
		// Tanpa Expire di sini: percobaan ke-max sudah membatalkan challenge bila salah, atau sedang
		// memakainya bila benar. Expire dari percobaan berlebih yang berbarengan bisa mendahului Consume
		// milik percobaan yang sah dan membuat login yang benar gagal.
		return nil, errOTPTooMany
	}
	if !hmac.Equal(v.codes.Hash(c.ID, in.Code), c.CodeHash) {
		if n >= v.cfg.MaxAttempts {
			_ = v.otps.Expire(ctx, c.ID) // challenge dibatalkan; user harus meminta kode baru
			return nil, errOTPTooMany
		}
		return nil, errOTPInvalid(v.cfg.MaxAttempts - n)
	}
	var newHash string
	if c.Purpose == domain.OTPPurposeReset {
		// hash hanya setelah kode terbukti benar (bcrypt mahal)
		if newHash, err = v.hasher.Hash(in.NewPassword); err != nil {
			return nil, err
		}
	}
	return v.login(ctx, c, newHash, in.ClientIP, now)
}

// login memakai challenge, menerapkan registrasi atau password baru, dan membuat sesi dalam satu
// transaksi.
func (v *OTPVerify) login(ctx context.Context, c *domain.OTPChallenge, newPasswordHash string, clientIP *netip.Addr, now time.Time) (*LoginResult, error) {
	token, hash, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	s := &domain.Session{ID: uuid.New(), UserID: c.UserID, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(v.cfg.SessionTTL), ClientIP: clientIP}
	var user *domain.User
	err = v.tx.WithTx(ctx, func(ctx context.Context) error {
		ok, err := v.otps.Consume(ctx, c.ID)
		if err != nil {
			return err
		}
		if !ok {
			return errOTPExpired // sudah dipakai oleh verifikasi lain yang berbarengan
		}
		switch c.Purpose {
		case domain.OTPPurposeReset:
			if err := v.users.SetPassword(ctx, c.UserID, newPasswordHash); err != nil {
				return err
			}
			// siapa pun yang memegang sesi lama (mis. perangkat hilang) harus login ulang
			if err := v.sessions.RevokeAllForUser(ctx, c.UserID, now); err != nil {
				return err
			}
			if err := v.users.TouchLogin(ctx, c.UserID, now); err != nil {
				return err
			}
		default:
			if err := v.users.CompleteRegistration(ctx, c.UserID, c.RegName, c.RegEmail, c.RegPasswordHash, now); err != nil {
				return err
			}
		}
		if err := v.sessions.Create(ctx, s); err != nil {
			return err
		}
		user, err = v.users.Get(ctx, c.UserID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &LoginResult{Token: token, ExpiresAt: s.ExpiresAt, User: user}, nil
}

// verifyDecoy meniru challenge asli untuk id palsu (nomor tak terdaftar): belum terkirim → 409,
// lalu salah terus sampai batas percobaan, lalu kedaluwarsa. Id yang sama sekali tidak dikenal
// dianggap kedaluwarsa (seperti challenge asli yang sudah dibersihkan).
func (v *OTPVerify) verifyDecoy(id uuid.UUID, now time.Time) error {
	d, ok := v.decoys.get(id)
	switch {
	case !ok, d.burned:
		return errOTPExpired
	case now.Before(d.sentAt):
		return errOTPNotSent
	case !now.Before(d.sentAt.Add(v.decoyTTL())):
		return errOTPExpired
	}
	n := v.decoys.attempt(id, v.cfg.MaxAttempts)
	switch {
	case n > v.cfg.MaxAttempts:
		return errOTPExpired // challenge asli sudah expired setelah batas tercapai
	case n == v.cfg.MaxAttempts:
		return errOTPTooMany
	}
	return errOTPInvalid(v.cfg.MaxAttempts - n)
}

func (v *OTPVerify) decoyTTL() time.Duration { return v.decoys.ttl }

// newSessionToken membuat token acak 32 byte (base64url) dan hash SHA-256 yang disimpan.
func newSessionToken() (string, []byte, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashSessionToken(token), nil
}

func hashSessionToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
