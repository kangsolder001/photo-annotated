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

// DeletionRepo menjalankan hapus permanen admin (spec-labels-admin §5.5).
type DeletionRepo struct{ base }

var _ usecase.DeletionRepo = (*DeletionRepo)(nil)

func NewDeletionRepo(pool *pgxpool.Pool) *DeletionRepo { return &DeletionRepo{base{pool}} }

// lockSubmission mengunci baris submission yang belum dihapus.
func (r *DeletionRepo) lockSubmission(ctx context.Context, id uuid.UUID) error {
	var got uuid.UUID
	err := r.db(ctx).QueryRow(ctx, `SELECT id FROM submissions WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("submission %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("postgres: lock submission: %w", err)
	}
	return nil
}

// markAll menandai submission deleted_at, semua fotonya deleted, dan menghapus semua anotasinya.
func (r *DeletionRepo) markAll(ctx context.Context, m *usecase.DeletionMark, now time.Time) error {
	tag, err := r.db(ctx).Exec(ctx, `DELETE FROM annotations a USING photos p WHERE a.photo_id = p.id AND p.submission_id = $1`, m.SubmissionID)
	if err != nil {
		return fmt.Errorf("postgres: delete annotations: %w", err)
	}
	m.AnnotationsDeleted += int(tag.RowsAffected())
	tag, err = r.db(ctx).Exec(ctx, `UPDATE photos SET status = 'deleted' WHERE submission_id = $1 AND status = 'attached'`, m.SubmissionID)
	if err != nil {
		return fmt.Errorf("postgres: mark photos deleted: %w", err)
	}
	m.PhotosDeleted += int(tag.RowsAffected())
	// foto lain yang masih punya baris (mis. dibuang lewat edit, menunggu cleanup) ikut dibersihkan
	if _, err := r.db(ctx).Exec(ctx, `UPDATE photos SET status = 'deleted' WHERE submission_id = $1 AND status <> 'deleted'`, m.SubmissionID); err != nil {
		return fmt.Errorf("postgres: mark photos deleted: %w", err)
	}
	if _, err := r.db(ctx).Exec(ctx, `UPDATE submissions SET deleted_at = $2 WHERE id = $1`, m.SubmissionID, now); err != nil {
		return fmt.Errorf("postgres: mark submission deleted: %w", err)
	}
	m.SubmissionDeleted = true
	return r.files(ctx, m)
}

// files mengisi daftar file yang perlu dihapus: semua baris foto deleted milik submission itu.
func (r *DeletionRepo) files(ctx context.Context, m *usecase.DeletionMark) error {
	rows, err := r.db(ctx).Query(ctx, `SELECT id, storage_key FROM photos WHERE submission_id = $1 AND status = 'deleted' ORDER BY id`, m.SubmissionID)
	if err != nil {
		return fmt.Errorf("postgres: list deleted photos: %w", err)
	}
	defer rows.Close()
	m.Files = m.Files[:0]
	for rows.Next() {
		var ref usecase.PhotoRef
		if err := rows.Scan(&ref.ID, &ref.StorageKey); err != nil {
			return fmt.Errorf("postgres: scan deleted photo: %w", err)
		}
		m.Files = append(m.Files, ref)
	}
	return rows.Err()
}

func (r *DeletionRepo) MarkSubmission(ctx context.Context, id uuid.UUID, now time.Time) (*usecase.DeletionMark, error) {
	if err := r.lockSubmission(ctx, id); err != nil {
		return nil, err
	}
	m := &usecase.DeletionMark{SubmissionID: id}
	if err := r.markAll(ctx, m, now); err != nil {
		return nil, err
	}
	return m, nil
}

func (r *DeletionRepo) MarkPhoto(ctx context.Context, photoID uuid.UUID, now time.Time) (*usecase.DeletionMark, error) {
	var sid uuid.UUID
	err := r.db(ctx).QueryRow(ctx, `SELECT submission_id FROM photos WHERE id = $1 AND status = 'attached'`, photoID).Scan(&sid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("photo %s: %w", photoID, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get photo: %w", err)
	}
	if err := r.lockSubmission(ctx, sid); err != nil {
		return nil, err
	}
	m := &usecase.DeletionMark{SubmissionID: sid}
	// diperiksa ulang setelah kunci: edit yang baru selesai bisa saja sudah membuang foto ini
	tag, err := r.db(ctx).Exec(ctx, `UPDATE photos SET status = 'deleted' WHERE id = $1 AND submission_id = $2 AND status = 'attached'`, photoID, sid)
	if err != nil {
		return nil, fmt.Errorf("postgres: mark photo deleted: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("photo %s: %w", photoID, domain.ErrNotFound)
	}
	m.PhotosDeleted = 1
	tag, err = r.db(ctx).Exec(ctx, `DELETE FROM annotations WHERE photo_id = $1`, photoID)
	if err != nil {
		return nil, fmt.Errorf("postgres: delete annotations: %w", err)
	}
	m.AnnotationsDeleted = int(tag.RowsAffected())
	var remaining int
	if err := r.db(ctx).QueryRow(ctx, `SELECT count(*) FROM photos WHERE submission_id = $1 AND status = 'attached'`, sid).Scan(&remaining); err != nil {
		return nil, fmt.Errorf("postgres: count photos: %w", err)
	}
	if remaining == 0 {
		// foto terakhir: submission ikut dihapus
		if err := r.markAll(ctx, m, now); err != nil {
			return nil, err
		}
		return m, nil
	}
	// tab edit pengguna yang terbuka mendapat 409 VERSION_CONFLICT lalu memuat ulang
	if _, err := r.db(ctx).Exec(ctx, `UPDATE submissions SET version = version + 1, updated_at = $2 WHERE id = $1`, sid, now); err != nil {
		return nil, fmt.Errorf("postgres: bump version: %w", err)
	}
	if _, err := r.db(ctx).Exec(ctx, `
		UPDATE photos p SET position = o.rn - 1
		  FROM (SELECT id, row_number() OVER (ORDER BY position, id) AS rn
		          FROM photos WHERE submission_id = $1 AND status = 'attached') o
		 WHERE p.id = o.id AND p.position <> o.rn - 1`, sid); err != nil {
		return nil, fmt.Errorf("postgres: compact positions: %w", err)
	}
	m.Files = []usecase.PhotoRef{}
	var key string
	if err := r.db(ctx).QueryRow(ctx, `SELECT storage_key FROM photos WHERE id = $1`, photoID).Scan(&key); err != nil {
		return nil, fmt.Errorf("postgres: photo key: %w", err)
	}
	m.Files = append(m.Files, usecase.PhotoRef{ID: photoID, StorageKey: key})
	return m, nil
}

func (r *DeletionRepo) DeletePhotoRow(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db(ctx).Exec(ctx, `DELETE FROM photos WHERE id = $1 AND status = 'deleted'`, id); err != nil {
		return fmt.Errorf("postgres: delete photo row: %w", err)
	}
	return nil
}

func (r *DeletionRepo) DeleteSubmissionIfEmpty(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `DELETE FROM submissions s WHERE s.id = $1 AND s.deleted_at IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM photos p WHERE p.submission_id = s.id)`, id)
	if err != nil {
		return false, fmt.Errorf("postgres: delete submission: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *DeletionRepo) MarkUserSubmissions(ctx context.Context, userID uuid.UUID, includeAccepted bool, now time.Time) (usecase.UserDataMark, error) {
	var m usecase.UserDataMark
	rows, err := r.db(ctx).Query(ctx, `SELECT id FROM submissions
		WHERE owner_id = $1 AND deleted_at IS NULL AND ($2 OR review_status <> 'accepted')
		ORDER BY id FOR UPDATE`, userID, includeAccepted)
	if err != nil {
		return m, fmt.Errorf("postgres: lock user submissions: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return m, fmt.Errorf("postgres: scan submission id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return m, err
	}
	if len(ids) == 0 {
		return m, nil
	}
	m.Submissions = len(ids)
	tag, err := r.db(ctx).Exec(ctx, `DELETE FROM annotations a USING photos p WHERE a.photo_id = p.id AND p.submission_id = ANY($1)`, ids)
	if err != nil {
		return m, fmt.Errorf("postgres: delete annotations: %w", err)
	}
	m.Annotations = int(tag.RowsAffected())
	tag, err = r.db(ctx).Exec(ctx, `UPDATE photos SET status = 'deleted' WHERE submission_id = ANY($1) AND status = 'attached'`, ids)
	if err != nil {
		return m, fmt.Errorf("postgres: mark photos deleted: %w", err)
	}
	m.Photos = int(tag.RowsAffected())
	if _, err := r.db(ctx).Exec(ctx, `UPDATE photos SET status = 'deleted' WHERE submission_id = ANY($1) AND status <> 'deleted'`, ids); err != nil {
		return m, fmt.Errorf("postgres: mark photos deleted: %w", err)
	}
	if _, err := r.db(ctx).Exec(ctx, `UPDATE submissions SET deleted_at = $2 WHERE id = ANY($1)`, ids, now); err != nil {
		return m, fmt.Errorf("postgres: mark submissions deleted: %w", err)
	}
	return m, nil
}
