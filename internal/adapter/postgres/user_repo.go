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

type UserRepo struct{ base }

var _ usecase.UserRepo = (*UserRepo)(nil)

func NewUserRepo(pool *pgxpool.Pool) *UserRepo { return &UserRepo{base{pool}} }

const userColumns = `id, name, email, phone, status, phone_verified_at, created_at, last_login_at`

func scanUser(row pgx.Row) (*domain.User, error) {
	var (
		u      domain.User
		status string
	)
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &status, &u.PhoneVerifiedAt, &u.CreatedAt, &u.LastLoginAt); err != nil {
		return nil, err
	}
	u.Status = domain.UserStatus(status)
	return &u, nil
}

func (r *UserRepo) FindOrCreatePending(ctx context.Context, u *domain.User) (*domain.User, bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		INSERT INTO users (id, name, email, phone, status, created_at)
		VALUES ($1, $2, $3, $4, 'pending', $5)
		ON CONFLICT (phone) DO NOTHING`, u.ID, u.Name, u.Email, u.Phone, u.CreatedAt)
	if err != nil {
		return nil, false, fmt.Errorf("postgres: insert user: %w", err)
	}
	if tag.RowsAffected() == 1 {
		created := *u
		created.Status = domain.UserPending
		return &created, true, nil
	}
	existing, err := r.GetByPhone(ctx, u.Phone)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

func (r *UserRepo) Get(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	u, err := scanUser(r.db(ctx).QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	return wrapUser(u, err, id.String())
}

func (r *UserRepo) GetByPhone(ctx context.Context, phone string) (*domain.User, error) {
	u, err := scanUser(r.db(ctx).QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE phone = $1`, phone))
	return wrapUser(u, err, "phone")
}

func wrapUser(u *domain.User, err error, what string) (*domain.User, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user %s: %w", what, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get user: %w", err)
	}
	return u, nil
}

func (r *UserRepo) Activate(ctx context.Context, id uuid.UUID, now time.Time) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE users
		   SET status = 'active', phone_verified_at = COALESCE(phone_verified_at, $2), last_login_at = $2
		 WHERE id = $1`, id, now)
	if err != nil {
		return fmt.Errorf("postgres: activate user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *UserRepo) DeletePendingBefore(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		DELETE FROM users WHERE id IN (
			SELECT id FROM users WHERE status = 'pending' AND created_at < $1
			 ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED
		)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete pending users: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
