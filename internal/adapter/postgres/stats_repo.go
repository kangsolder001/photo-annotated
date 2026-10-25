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

var _ usecase.AdminStatsRepo = (*StatsRepo)(nil)

// LabelStats menghitung kotak/foto/submission per label dalam satu query GROUP BY (indeks
// annotations(label_id)); hanya foto attached pada submission yang belum dihapus.
func (r *StatsRepo) LabelStats(ctx context.Context, f usecase.LabelStatsFilter) (usecase.LabelStatsTotals, []usecase.LabelStat, error) {
	var tot usecase.LabelStatsTotals
	review := reviewArg(f.Review)
	err := r.db(ctx).QueryRow(ctx, `
		WITH ph AS (
			SELECT p.id, p.submission_id
			  FROM photos p JOIN submissions s ON s.id = p.submission_id
			 WHERE p.status = 'attached' AND s.deleted_at IS NULL
			   AND ($1::timestamptz IS NULL OR s.created_at >= $1)
			   AND ($2::timestamptz IS NULL OR s.created_at < $2)
			   AND ($3::text IS NULL OR s.review_status = $3)
		), an AS (
			SELECT a.label_id FROM annotations a JOIN ph ON ph.id = a.photo_id
		)
		SELECT (SELECT count(DISTINCT submission_id) FROM ph), (SELECT count(*) FROM ph),
		       (SELECT count(*) FROM an), (SELECT count(*) FROM an WHERE label_id IS NULL)`, f.From, f.To, review).
		Scan(&tot.Submissions, &tot.Photos, &tot.Annotations, &tot.LegacyTextAnnotations)
	if err != nil {
		return tot, nil, fmt.Errorf("postgres: label stats totals: %w", err)
	}
	rows, err := r.db(ctx).Query(ctx, `
		WITH a AS (
			SELECT a.label_id, p.id AS photo_id, s.id AS submission_id
			  FROM annotations a JOIN photos p ON p.id = a.photo_id JOIN submissions s ON s.id = p.submission_id
			 WHERE a.label_id IS NOT NULL AND p.status = 'attached' AND s.deleted_at IS NULL
			   AND ($1::timestamptz IS NULL OR s.created_at >= $1)
			   AND ($2::timestamptz IS NULL OR s.created_at < $2)
			   AND ($3::text IS NULL OR s.review_status = $3)
		)
		SELECT l.id, l.name, l.description, l.color, l.sort_order, l.archived_at, l.created_at, l.updated_at,
		       count(a.label_id), count(DISTINCT a.photo_id), count(DISTINCT a.submission_id)
		  FROM labels l LEFT JOIN a ON a.label_id = l.id
		 GROUP BY l.id
		 ORDER BY count(a.label_id) DESC, (l.archived_at IS NOT NULL), l.sort_order, lower(l.name), l.id`, f.From, f.To, review)
	if err != nil {
		return tot, nil, fmt.Errorf("postgres: label stats: %w", err)
	}
	defer rows.Close()
	out := []usecase.LabelStat{}
	for rows.Next() {
		var st usecase.LabelStat
		l, err := scanLabel(rows, &st.Annotations, &st.Photos, &st.Submissions)
		if err != nil {
			return tot, nil, fmt.Errorf("postgres: scan label stat: %w", err)
		}
		st.Label = *l
		out = append(out, st)
	}
	return tot, out, rows.Err()
}

func (r *StatsRepo) DataCounts(ctx context.Context, dueBefore time.Time) (usecase.DataCounts, error) {
	var c usecase.DataCounts
	err := r.db(ctx).QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE deleted_at IS NULL),
		       count(*) FILTER (WHERE deleted_at IS NULL AND review_status = 'pending'),
		       count(*) FILTER (WHERE deleted_at IS NULL AND review_status = 'accepted'),
		       count(*) FILTER (WHERE deleted_at IS NULL AND review_status = 'rejected'),
		       count(*) FILTER (WHERE deleted_at IS NULL AND review_status <> 'accepted' AND created_at < $1),
		       (SELECT count(*) FROM users WHERE status = 'active' AND blocked_at IS NULL),
		       (SELECT count(*) FROM users WHERE status = 'active' AND blocked_at IS NOT NULL),
		       (SELECT count(*) FROM photos WHERE status = 'deleted')
		  FROM submissions`, dueBefore).
		Scan(&c.Submissions, &c.ReviewPending, &c.Accepted, &c.Rejected, &c.RetentionDue7d, &c.UsersActive, &c.UsersBlocked, &c.FilesPendingDelete)
	if err != nil {
		return c, fmt.Errorf("postgres: data counts: %w", err)
	}
	return c, nil
}
