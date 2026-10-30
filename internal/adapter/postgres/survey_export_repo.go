package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// SurveyExportRepo membaca pendataan untuk export CSV (spec-pendataan §5.7). Sengaja tidak memilih nomor
// HP, email, atau IP.
type SurveyExportRepo struct{ base }

var _ usecase.SurveyExportRepo = (*SurveyExportRepo)(nil)

func NewSurveyExportRepo(pool *pgxpool.Pool) *SurveyExportRepo { return &SurveyExportRepo{base{pool}} }

func (r *SurveyExportRepo) SurveyExportPage(ctx context.Context, after *usecase.AdminCursor, f usecase.SurveyExportFilter, limit int) ([]usecase.SurveyExportRow, error) {
	var (
		aCreated *time.Time
		aID      *uuid.UUID
	)
	if after != nil {
		aCreated, aID = &after.CreatedAt, &after.ID
	}
	rows, err := r.db(ctx).Query(ctx, `
		SELECT s.id, s.created_at, s.review_status, s.form_version, s.answers, u.name, u.nim, g.name,
		       (SELECT count(*) FROM photos p WHERE p.submission_id = s.id AND p.status = 'attached'),
		       (SELECT count(*) FROM annotations a JOIN photos p ON p.id = a.photo_id
		         WHERE p.submission_id = s.id AND p.status = 'attached')
		  FROM submissions s
		  LEFT JOIN users u  ON u.id = s.owner_id
		  LEFT JOIN groups g ON g.id = u.group_id
		 WHERE s.deleted_at IS NULL AND s.answers IS NOT NULL
		   AND ($1::timestamptz IS NULL OR s.created_at >= $1)
		   AND ($2::timestamptz IS NULL OR s.created_at < $2)
		   AND ($3::timestamptz IS NULL OR (s.created_at, s.id) > ($3, $4::uuid))
		   AND ($5::boolean IS NULL OR s.consent = $5)
		   AND ($6::text IS NULL OR s.review_status = $6)
		 ORDER BY s.created_at, s.id
		 LIMIT $7`, f.From, f.To, aCreated, aID, f.Consent, reviewArg(f.Review), limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: survey export page: %w", err)
	}
	defer rows.Close()
	var out []usecase.SurveyExportRow
	for rows.Next() {
		var (
			e                   usecase.SurveyExportRow
			review              string
			fv                  *int
			name, nim, groupNam *string
		)
		s := &e.Submission
		if err := rows.Scan(&s.ID, &s.CreatedAt, &review, &fv, &s.Answers, &name, &nim, &groupNam, &e.PhotoCount, &e.AnnotationCount); err != nil {
			return nil, fmt.Errorf("postgres: scan survey export row: %w", err)
		}
		s.Review.Status = domain.ReviewStatus(review)
		if fv != nil {
			s.FormVersion = *fv
		}
		e.Student, e.NIM, e.Group = deref(name), deref(nim), deref(groupNam)
		out = append(out, e)
	}
	return out, rows.Err()
}
