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

// ExportRepo membaca foto attached beserta anotasinya untuk export. Sengaja tidak memilih kolom
// pemilik atau IP: data pribadi tidak pernah keluar lewat jalur ini.
type ExportRepo struct{ base }

var _ usecase.ExportRepo = (*ExportRepo)(nil)

func NewExportRepo(pool *pgxpool.Pool) *ExportRepo { return &ExportRepo{base{pool}} }

func (r *ExportRepo) ExportPage(ctx context.Context, after *usecase.ExportCursor, f usecase.ExportFilter, limit int) ([]usecase.ExportPhoto, error) {
	var (
		aCreated *time.Time
		aSub     *uuid.UUID
		aPos     *int
		aPhoto   *uuid.UUID
	)
	if after != nil {
		aCreated, aSub, aPos, aPhoto = &after.CreatedAt, &after.SubmissionID, &after.Position, &after.PhotoID
	}
	rows, err := r.db(ctx).Query(ctx, `
		SELECT s.id, s.created_at, s.review_status, p.id, p.position, p.storage_key, p.content_type, p.size_bytes, p.width, p.height, p.sha256
		  FROM photos p
		  JOIN submissions s ON s.id = p.submission_id
		 WHERE p.status = 'attached' AND s.deleted_at IS NULL
		   AND ($1::timestamptz IS NULL OR s.created_at >= $1)
		   AND ($2::timestamptz IS NULL OR s.created_at < $2)
		   AND ($3::timestamptz IS NULL OR (s.created_at, s.id, p.position, p.id) > ($3, $4::uuid, $5::smallint, $6::uuid))
		   AND ($8::int IS NULL OR EXISTS (SELECT 1 FROM annotations fa WHERE fa.photo_id = p.id AND fa.label_id = $8))
		   AND ($9::text IS NULL OR s.review_status = $9)
		 ORDER BY s.created_at, s.id, p.position, p.id
		 LIMIT $7`, f.From, f.To, aCreated, aSub, aPos, aPhoto, limit, f.LabelID, reviewArg(f.Review))
	if err != nil {
		return nil, fmt.Errorf("postgres: export page: %w", err)
	}
	defer rows.Close()
	var (
		out   []usecase.ExportPhoto
		ids   []uuid.UUID
		index = map[uuid.UUID]int{}
	)
	for rows.Next() {
		var (
			e   usecase.ExportPhoto
			pos int16
		)
		var review string
		if err := rows.Scan(&e.SubmissionID, &e.CreatedAt, &review, &e.Photo.ID, &pos, &e.Photo.StorageKey, &e.Photo.ContentType,
			&e.Photo.SizeBytes, &e.Photo.Width, &e.Photo.Height, &e.Photo.SHA256); err != nil {
			return nil, fmt.Errorf("postgres: scan export row: %w", err)
		}
		e.Photo.Position = int(pos)
		e.Review = domain.ReviewStatus(review)
		e.Photo.SubmissionID = &e.SubmissionID
		e.Photo.Status = domain.PhotoAttached
		index[e.Photo.ID] = len(out)
		ids = append(ids, e.Photo.ID)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: export rows: %w", err)
	}
	if len(ids) == 0 {
		return out, nil
	}
	arows, err := r.db(ctx).Query(ctx,
		`SELECT a.photo_id, a.label_id, a.text, a.x, a.y, a.w, a.h, l.name, l.color, l.archived_at
		   FROM annotations a LEFT JOIN labels l ON l.id = a.label_id
		  WHERE a.photo_id = ANY($1) ORDER BY a.photo_id, a.id`, ids)
	if err != nil {
		return nil, fmt.Errorf("postgres: export annotations: %w", err)
	}
	defer arows.Close()
	for arows.Next() {
		var (
			pid               uuid.UUID
			labelID           *int
			text, name, color *string
			archivedAt        *time.Time
			x, y, w, h        float64
		)
		if err := arows.Scan(&pid, &labelID, &text, &x, &y, &w, &h, &name, &color, &archivedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan export annotation: %w", err)
		}
		i := index[pid]
		out[i].Annotations = append(out[i].Annotations, annotationFrom(labelID, text, name, color, archivedAt, x, y, w, h))
	}
	if err := arows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: export annotations rows: %w", err)
	}
	return out, nil
}

func reviewArg(r *domain.ReviewStatus) *string {
	if r == nil {
		return nil
	}
	s := string(*r)
	return &s
}
