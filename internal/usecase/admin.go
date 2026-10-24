package usecase

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"time"

	"golang.org/x/crypto/bcrypt"

	"photo-annotated/internal/domain"
)

// AdminConfig adalah akun admin statis dari config.json (spec §5.9, §9.1).
type AdminConfig struct {
	Username       string
	PasswordBcrypt string
	TokenTTL       time.Duration
}

// AdminAuth memeriksa login admin dan token admin.
type AdminAuth struct {
	userHash [32]byte
	pwHash   []byte
	tokens   AdminTokenSigner
	clock    Clock
	ttl      time.Duration
}

func NewAdminAuth(tokens AdminTokenSigner, clock Clock, cfg AdminConfig) *AdminAuth {
	if cfg.Username == "" || cfg.PasswordBcrypt == "" || cfg.TokenTTL <= 0 {
		panic("usecase: AdminConfig tidak valid")
	}
	return &AdminAuth{userHash: sha256.Sum256([]byte(cfg.Username)), pwHash: []byte(cfg.PasswordBcrypt), tokens: tokens, clock: clock, ttl: cfg.TokenTTL}
}

var errInvalidCredentials = domain.Unauthorized("INVALID_CREDENTIALS", "invalid username or password")

// Login mengembalikan token admin bila kredensial benar. Username dibandingkan constant-time (atas
// hash-nya, agar panjangnya pun tidak bocor) dan bcrypt selalu dijalankan, sehingga username salah
// dan password salah memakan waktu dan menghasilkan pesan yang sama.
func (a *AdminAuth) Login(_ context.Context, username, password string) (token string, expiresAt time.Time, err error) {
	u := sha256.Sum256([]byte(username))
	userOK := subtle.ConstantTimeCompare(u[:], a.userHash[:]) == 1
	pwOK := bcrypt.CompareHashAndPassword(a.pwHash, []byte(password)) == nil
	if !userOK || !pwOK {
		return "", time.Time{}, errInvalidCredentials
	}
	expiresAt = a.clock.Now().Add(a.ttl)
	return a.tokens.Issue(expiresAt), expiresAt, nil
}

// VerifyToken: true bila token adalah token admin yang sah dan belum kedaluwarsa.
func (a *AdminAuth) VerifyToken(token string) bool {
	return a.tokens.Verify(token, a.clock.Now())
}
