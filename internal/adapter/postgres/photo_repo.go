package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type PhotoRepo struct{ pool *pgxpool.Pool }

var _ usecase.PhotoRepo = (*PhotoRepo)(nil)

func NewPhotoRepo(pool *pgxpool.Pool) *PhotoRepo { return &PhotoRepo{pool: pool} }

const photoColumns = `id, submission_id, owner_id, position, storage_key, content_type, size_bytes,
	width, height, uploaded_at, sha256, status, created_at`

func (r *PhotoRepo) Create(ctx context.Context, p *domain.Photo) error {
	_, err := r.pool.Exec(ctx, `
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
	row := r.pool.QueryRow(ctx, `SELECT `+photoColumns+` FROM photos WHERE id = $1`, id)
	p, err := scanPhoto(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("photo %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get photo: %w", err)
	}
	return p, nil
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
