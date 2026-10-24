package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/usecase"
)

type StatsRepo struct{ base }

var _ usecase.StatsRepo = (*StatsRepo)(nil)

func NewStatsRepo(pool *pgxpool.Pool) *StatsRepo { return &StatsRepo{base{pool}} }

// Counts memakai indeks (status, created_at) dan (status, next_attempt_at) yang sudah ada.
func (r *StatsRepo) Counts(ctx context.Context, since time.Time) (usecase.StatsCounts, error) {
	var c usecase.StatsCounts
	err := r.db(ctx).QueryRow(ctx, `
		SELECT (SELECT count(*) FROM photos WHERE status = 'pending'),
		       (SELECT count(*) FROM otp_challenges WHERE status IN ('queued', 'sending')),
		       (SELECT count(*) FROM otp_challenges WHERE created_at >= $1),
		       (SELECT count(*) FROM otp_challenges WHERE created_at >= $1 AND sent_at IS NOT NULL),
		       (SELECT count(*) FROM otp_challenges WHERE created_at >= $1 AND status = 'failed')`, since).
		Scan(&c.PhotosPending, &c.OTPQueued, &c.OTPCreated24h, &c.OTPSent24h, &c.OTPFailed24h)
	if err != nil {
		return c, fmt.Errorf("postgres: stats counts: %w", err)
	}
	return c, nil
}
