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

type PhotoRepo struct{ base }

var _ usecase.PhotoRepo = (*PhotoRepo)(nil)

func NewPhotoRepo(pool *pgxpool.Pool) *PhotoRepo { return &PhotoRepo{base{pool}} }

const photoColumns = `id, submission_id, owner_id, position, storage_key, content_type, size_bytes,
	width, height, uploaded_at, sha256, status, created_at`

func (r *PhotoRepo) Create(ctx context.Context, p *domain.Photo) error {
	_, err := r.db(ctx).Exec(ctx, `
		INSERT INTO photos (`+photoColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		p.ID, p.SubmissionID, p.OwnerID, p.Position, p.StorageKey, p.ContentType, p.SizeBytes,
		p.Width, p.Height, p.UploadedAt, p.SHA256, string(p.Status), p.CreatedAt)
	if err != nil {
		return fmt.Errorf("postgres: insert photo: %w", err)
	}
	return nil
}

func (r *PhotoRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Photo, error) {
	row := r.db(ctx).QueryRow(ctx, `SELECT `+photoColumns+` FROM photos WHERE id = $1`, id)
	p, err := scanPhoto(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("photo %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get photo: %w", err)
	}
	return p, nil
}

func (r *PhotoRepo) MarkUploaded(ctx context.Context, id uuid.UUID, at time.Time, sha256 []byte) error {
	tag, err := r.db(ctx).Exec(ctx,
		`UPDATE photos SET uploaded_at = $2, sha256 = $3 WHERE id = $1 AND status = 'pending'`, id, at, sha256)
	if err != nil {
		return fmt.Errorf("postgres: mark uploaded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo %s is no longer pending", id)
	}
	return nil
}

func (r *PhotoRepo) MarkRejected(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db(ctx).Exec(ctx, `UPDATE photos SET status = 'rejected' WHERE id = $1 AND status = 'pending'`, id); err != nil {
		return fmt.Errorf("postgres: mark rejected: %w", err)
	}
	return nil
}

func (r *PhotoRepo) Attach(ctx context.Context, photoID, submissionID uuid.UUID, position int, ownerID *uuid.UUID) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE photos
		   SET submission_id = $2, position = $3, status = 'attached'
		 WHERE id = $1
		   AND status = 'pending'
		   AND uploaded_at IS NOT NULL
		   AND owner_id IS NOT DISTINCT FROM $4::uuid`,
		photoID, submissionID, position, ownerID)
	if err != nil {
		return fmt.Errorf("postgres: attach photo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo %s cannot be attached to this submission", photoID)
	}
	return nil
}

func (r *PhotoRepo) MarkDeleted(ctx context.Context, photoID, submissionID uuid.UUID) error {
	tag, err := r.db(ctx).Exec(ctx,
		`UPDATE photos SET status = 'deleted' WHERE id = $1 AND submission_id = $2 AND status = 'attached'`, photoID, submissionID)
	if err != nil {
		return fmt.Errorf("postgres: mark deleted: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo %s is not attached to this submission", photoID)
	}
	return nil
}

func (r *PhotoRepo) SetPosition(ctx context.Context, photoID, submissionID uuid.UUID, position int) error {
	tag, err := r.db(ctx).Exec(ctx,
		`UPDATE photos SET position = $3 WHERE id = $1 AND submission_id = $2 AND status = 'attached'`, photoID, submissionID, position)
	if err != nil {
		return fmt.Errorf("postgres: set position: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo %s is not attached to this submission", photoID)
	}
	return nil
}

func (r *PhotoRepo) ListBySubmission(ctx context.Context, submissionID uuid.UUID) ([]*domain.Photo, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+photoColumns+` FROM photos
		WHERE submission_id = $1 AND status = 'attached' ORDER BY position, id`, submissionID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list photos: %w", err)
	}
	defer rows.Close()
	var out []*domain.Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan photo: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPhoto(row pgx.Row) (*domain.Photo, error) {
	var (
		p      domain.Photo
		status string
	)
	err := row.Scan(&p.ID, &p.SubmissionID, &p.OwnerID, &p.Position, &p.StorageKey, &p.ContentType,
		&p.SizeBytes, &p.Width, &p.Height, &p.UploadedAt, &p.SHA256, &status, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.Status = domain.PhotoStatus(status)
	return &p, nil
}
