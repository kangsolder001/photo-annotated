package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/usecase"
)

type CleanupRepo struct{ base }

var _ usecase.CleanupRepo = (*CleanupRepo)(nil)

func NewCleanupRepo(pool *pgxpool.Pool) *CleanupRepo { return &CleanupRepo{base{pool}} }

func (r *CleanupRepo) ClaimStalePending(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		WITH stale AS (
			SELECT id FROM photos
			 WHERE status = 'pending' AND created_at < $1
			 ORDER BY created_at
			 LIMIT $2
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE photos p SET status = 'deleted' FROM stale WHERE p.id = stale.id`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("postgres: claim stale pending: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *CleanupRepo) ListDeletablePhotos(ctx context.Context, afterID uuid.UUID, limit int) ([]usecase.PhotoRef, error) {
	rows, err := r.db(ctx).Query(ctx, `
		SELECT id, storage_key FROM photos
		 WHERE status IN ('rejected', 'deleted') AND id > $1
		 ORDER BY id
		 LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: list deletable photos: %w", err)
	}
	defer rows.Close()
	var out []usecase.PhotoRef
	for rows.Next() {
		var p usecase.PhotoRef
		if err := rows.Scan(&p.ID, &p.StorageKey); err != nil {
			return nil, fmt.Errorf("postgres: scan deletable photo: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *CleanupRepo) DeletePhotoRow(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db(ctx).Exec(ctx, `DELETE FROM photos WHERE id = $1 AND status IN ('rejected', 'deleted')`, id); err != nil {
		return fmt.Errorf("postgres: delete photo row: %w", err)
	}
	return nil
}

func (r *CleanupRepo) ExpireSubmissions(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		WITH expired AS (
			SELECT s.id FROM submissions s
			 WHERE s.created_at < $1
			   AND EXISTS (SELECT 1 FROM photos p WHERE p.submission_id = s.id AND p.status <> 'deleted')
			 ORDER BY s.created_at
			 LIMIT $2
			 FOR UPDATE OF s SKIP LOCKED
		)
		UPDATE photos p SET status = 'deleted'
		  FROM expired
		 WHERE p.submission_id = expired.id AND p.status <> 'deleted'`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("postgres: expire submissions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *CleanupRepo) DeleteEmptyExpiredSubmissions(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		DELETE FROM submissions
		 WHERE id IN (
			SELECT s.id FROM submissions s
			 WHERE s.created_at < $1
			   AND NOT EXISTS (SELECT 1 FROM photos p WHERE p.submission_id = s.id)
			 ORDER BY s.created_at
			 LIMIT $2
			 FOR UPDATE OF s SKIP LOCKED
		)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete expired submissions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *CleanupRepo) ExistingStorageKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	out := make(map[string]bool, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := r.db(ctx).Query(ctx, `SELECT storage_key FROM photos WHERE storage_key = ANY($1)`, keys)
	if err != nil {
		return nil, fmt.Errorf("postgres: existing storage keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("postgres: scan storage key: %w", err)
		}
		out[k] = true
	}
	return out, rows.Err()
}

// DeleteEmptyDeletedSubmissions menghapus submission yang dihapus admin (deleted_at) dan sudah tidak
// punya baris foto lagi (file-file-nya sudah terhapus lebih dulu).
func (r *CleanupRepo) DeleteEmptyDeletedSubmissions(ctx context.Context, limit int) (int, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		DELETE FROM submissions
		 WHERE id IN (
			SELECT s.id FROM submissions s
			 WHERE s.deleted_at IS NOT NULL
			   AND NOT EXISTS (SELECT 1 FROM photos p WHERE p.submission_id = s.id)
			 ORDER BY s.deleted_at
			 LIMIT $1
			 FOR UPDATE OF s SKIP LOCKED
		)`, limit)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete removed submissions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
