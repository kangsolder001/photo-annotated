package usecase

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// Sessions memvalidasi token bearer dan mengelola sesi login (auth.mode = otp).
type Sessions struct {
	sessions SessionRepo
	users    UserRepo
	clock    Clock
}

func NewSessions(sessions SessionRepo, users UserRepo, clock Clock) *Sessions {
	return &Sessions{sessions: sessions, users: users, clock: clock}
}

var errUnauthorized = domain.Unauthorized("UNAUTHORIZED", "missing, invalid, or expired session")

// Authenticate mengembalikan sesi aktif untuk token. Satu lookup indeks unik; tidak ada penulisan.
// Token yang tidak dikenal, dicabut, atau kedaluwarsa → Unauthorized.
func (s *Sessions) Authenticate(ctx context.Context, token string) (*domain.Session, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(token); err != nil || len(raw) != 32 {
		return nil, errUnauthorized
	}
	sess, err := s.sessions.GetByTokenHash(ctx, hashSessionToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if !sess.Active(s.clock.Now()) {
		return nil, errUnauthorized
	}
	return sess, nil
}

// Logout mencabut sesi (idempoten).
func (s *Sessions) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.sessions.Revoke(ctx, sessionID, s.clock.Now())
}

// Me mengembalikan profil pengguna sesi itu. Pengguna yang sudah dihapus → Unauthorized.
func (s *Sessions) Me(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	u, err := s.users.Get(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, errUnauthorized
	}
	return u, err
}
