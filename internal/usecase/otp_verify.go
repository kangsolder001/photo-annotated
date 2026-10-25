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
	if err := validCodeFormat(in.Code); err != nil {
		return nil, err
	}
	if in.NewPassword != "" {
		if err := domain.ValidatePassword(in.NewPassword); err != nil {
			return nil, err
		}
	}
	now := v.clock.Now()
	c, err := v.otps.Get(ctx, in.ChallengeID)
	if err == nil && c.Purpose == domain.OTPPurposeAdminLogin {
		err = domain.ErrNotFound // challenge admin hanya bisa dipakai lewat /admin/otp/verify
	}
	if errors.Is(err, domain.ErrNotFound) {
		if d, ok := v.decoys.get(in.ChallengeID); ok && d.purpose == domain.OTPPurposeReset && in.NewPassword == "" {
			return nil, errPasswordRequired
		}
		return nil, v.decoys.verify(in.ChallengeID, now, v.cfg.MaxAttempts)
	}
	if err != nil {
		return nil, err
	}
	// diperiksa sebelum percobaan dihitung: lupa mengisi password baru bukan tebakan kode
	if c.Purpose == domain.OTPPurposeReset && in.NewPassword == "" {
		return nil, errPasswordRequired
	}
	if err := checkCode(ctx, v.otps, v.codes, c, in.Code, v.cfg.MaxAttempts, now); err != nil {
		return nil, err
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
			// kode reset yang terkirim sebelum akun diblokir tidak boleh membuka akses lagi
			if u, err := v.users.Get(ctx, c.UserID); err != nil {
				return err
			} else if u.Blocked() {
				return errAccountBlocked
			}
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

// validCodeFormat: kode hanya angka, panjang wajar. Bentuk salah tidak dihitung sebagai percobaan.
func validCodeFormat(code string) error {
	if len(code) == 0 || len(code) > 16 {
		return errOTPBadInput
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return errOTPBadInput
		}
	}
	return nil
}

// checkCode memeriksa kode untuk challenge asli (dipakai verifikasi pengguna dan login admin).
// Percobaan dihitung secara atomik sebelum dibandingkan, sehingga tebakan paralel tidak dapat
// melewati batas. nil berarti kode benar; challenge belum dipakai (Consume tetap tugas pemanggil).
func checkCode(ctx context.Context, otps OTPRepo, codes OTPCodes, c *domain.OTPChallenge, code string, maxAttempts int, now time.Time) error {
	switch c.Status {
	case domain.OTPQueued, domain.OTPSending, domain.OTPFailed:
		return errOTPNotSent
	case domain.OTPSent:
	default: // expired, consumed
		return errOTPExpired
	}
	if c.ExpiresAt == nil || !now.Before(*c.ExpiresAt) {
		_ = otps.Expire(ctx, c.ID)
		return errOTPExpired
	}
	n, err := otps.IncrementVerifyAttempts(ctx, c.ID)
	if err != nil {
		return err
	}
	if n > maxAttempts {
		// Tanpa Expire di sini: percobaan ke-max sudah membatalkan challenge bila salah, atau sedang
		// memakainya bila benar. Expire dari percobaan berlebih yang berbarengan bisa mendahului Consume
		// milik percobaan yang sah dan membuat login yang benar gagal.
		return errOTPTooMany
	}
	if !hmac.Equal(codes.Hash(c.ID, code), c.CodeHash) {
		if n >= maxAttempts {
			_ = otps.Expire(ctx, c.ID) // challenge dibatalkan; user harus meminta kode baru
			return errOTPTooMany
		}
		return errOTPInvalid(maxAttempts - n)
	}
	return nil
}

// verify meniru challenge asli untuk id palsu (nomor tak terdaftar): belum terkirim → 409,
// lalu salah terus sampai batas percobaan, lalu kedaluwarsa. Id yang sama sekali tidak dikenal
// dianggap kedaluwarsa (seperti challenge asli yang sudah dibersihkan).
func (d *decoys) verify(id uuid.UUID, now time.Time, maxAttempts int) error {
	v, ok := d.get(id)
	switch {
	case !ok, v.burned:
		return errOTPExpired
	case now.Before(v.sentAt):
		return errOTPNotSent
	case !now.Before(v.sentAt.Add(d.ttl)):
		return errOTPExpired
	}
	n := d.attempt(id, maxAttempts)
	switch {
	case n > maxAttempts:
		return errOTPExpired // challenge asli sudah expired setelah batas tercapai
	case n == maxAttempts:
		return errOTPTooMany
	}
	return errOTPInvalid(maxAttempts - n)
}

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
