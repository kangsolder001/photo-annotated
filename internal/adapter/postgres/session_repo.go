package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type SessionRepo struct{ base }

var _ usecase.SessionRepo = (*SessionRepo)(nil)

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo { return &SessionRepo{base{pool}} }

func (r *SessionRepo) Create(ctx context.Context, s *domain.Session) error {
	if _, err := r.db(ctx).Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at, revoked_at, client_ip)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, s.ID, s.UserID, s.TokenHash, s.CreatedAt, s.ExpiresAt, s.RevokedAt, s.ClientIP); err != nil {
		return fmt.Errorf("postgres: insert session: %w", err)
	}
	return nil
}

func (r *SessionRepo) GetByTokenHash(ctx context.Context, hash []byte) (*domain.Session, error) {
	var s domain.Session
	err := r.db(ctx).QueryRow(ctx,
		`SELECT id, user_id, token_hash, created_at, expires_at, revoked_at, client_ip FROM sessions WHERE token_hash = $1`, hash).
		Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt, &s.ClientIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get session: %w", err)
	}
	return &s, nil
}

func (r *SessionRepo) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	if _, err := r.db(ctx).Exec(ctx, `UPDATE sessions SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, id, now); err != nil {
		return fmt.Errorf("postgres: revoke session: %w", err)
	}
	return nil
}

func (r *SessionRepo) DeleteInactiveBefore(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		DELETE FROM sessions WHERE id IN (
			SELECT id FROM sessions WHERE expires_at < $1 OR revoked_at < $1
			 ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED
		)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
