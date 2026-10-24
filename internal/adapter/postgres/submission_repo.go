package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type SubmissionRepo struct {
	base
	tx *TxManager
}

var _ usecase.SubmissionRepo = (*SubmissionRepo)(nil)

func NewSubmissionRepo(pool *pgxpool.Pool) *SubmissionRepo {
	return &SubmissionRepo{base: base{pool}, tx: NewTxManager(pool)}
}

const submissionColumns = `id, owner_id, client_request_id, client_ip, version, created_at, updated_at`

func (r *SubmissionRepo) Create(ctx context.Context, s *domain.Submission) error {
	_, err := r.db(ctx).Exec(ctx, `INSERT INTO submissions (`+submissionColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		s.ID, s.OwnerID, s.ClientRequestID, s.ClientIP, s.Version, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "client_request_id") {
			return domain.Conflict("DUPLICATE_CLIENT_REQUEST", "a submission with this client_request_id already exists")
		}
		return fmt.Errorf("postgres: insert submission: %w", err)
	}
	return nil
}

func (r *SubmissionRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Submission, error) {
	return r.getBy(ctx, "id", id)
}

func (r *SubmissionRepo) GetByClientRequestID(ctx context.Context, id uuid.UUID) (*domain.Submission, error) {
	return r.getBy(ctx, "client_request_id", id)
}

func (r *SubmissionRepo) getBy(ctx context.Context, column string, id uuid.UUID) (*domain.Submission, error) {
	var s domain.Submission
	// submission yang dihapus admin (deleted_at) tidak terlihat oleh siapa pun (spec-labels-admin §5.5)
	err := r.db(ctx).QueryRow(ctx, `SELECT `+submissionColumns+` FROM submissions WHERE `+column+` = $1 AND deleted_at IS NULL`, id).
		Scan(&s.ID, &s.OwnerID, &s.ClientRequestID, &s.ClientIP, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("submission %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get submission: %w", err)
	}
	return &s, nil
}

// GetDetail membaca submission, foto attached, dan anotasi dengan SATU statement sehingga
// hasilnya selalu snapshot yang konsisten, walau ada edit bersamaan di antara dua query.
func (r *SubmissionRepo) GetDetail(ctx context.Context, id uuid.UUID) (*domain.SubmissionDetail, error) {
	rows, err := r.db(ctx).Query(ctx, `
		SELECT s.id, s.owner_id, s.client_request_id, s.client_ip, s.version, s.created_at, s.updated_at,
		       p.id, p.submission_id, p.owner_id, p.position, p.storage_key, p.content_type, p.size_bytes,
		       p.width, p.height, p.uploaded_at, p.sha256, p.status, p.created_at,
		       a.id, a.label_id, a.text, a.x, a.y, a.w, a.h, l.name, l.color, l.archived_at
		  FROM submissions s
		  LEFT JOIN photos p      ON p.submission_id = s.id AND p.status = 'attached'
		  LEFT JOIN annotations a ON a.photo_id = p.id
		  LEFT JOIN labels l      ON l.id = a.label_id
		 WHERE s.id = $1 AND s.deleted_at IS NULL
		 ORDER BY p.position, p.id, a.id`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: get submission detail: %w", err)
	}
	defer rows.Close()

	var detail *domain.SubmissionDetail
	for rows.Next() {
		var (
			s                   domain.Submission
			pid, psub, powner   *uuid.UUID
			ppos, psize, pw, ph *int
			pkey, pct, pstatus  *string
			puploaded, pcreated *time.Time
			psha                []byte
			aid                 *int64
			alabel              *int
			atext, lname, lcol  *string
			ax, ay, aw, ah      *float64
			larchived           *time.Time
		)
		if err := rows.Scan(&s.ID, &s.OwnerID, &s.ClientRequestID, &s.ClientIP, &s.Version, &s.CreatedAt, &s.UpdatedAt,
			&pid, &psub, &powner, &ppos, &pkey, &pct, &psize, &pw, &ph, &puploaded, &psha, &pstatus, &pcreated,
			&aid, &alabel, &atext, &ax, &ay, &aw, &ah, &lname, &lcol, &larchived); err != nil {
			return nil, fmt.Errorf("postgres: scan submission detail: %w", err)
		}
		if detail == nil {
			detail = &domain.SubmissionDetail{Submission: s, Photos: []domain.PhotoWithAnnotations{}}
		}
		if pid == nil {
			continue // submission tanpa foto attached
		}
		if n := len(detail.Photos); n == 0 || detail.Photos[n-1].Photo.ID != *pid {
			detail.Photos = append(detail.Photos, domain.PhotoWithAnnotations{
				Photo: domain.Photo{
					ID: *pid, SubmissionID: psub, OwnerID: powner, Position: *ppos, StorageKey: *pkey,
					ContentType: *pct, SizeBytes: int64(*psize), Width: *pw, Height: *ph, UploadedAt: puploaded,
					SHA256: psha, Status: domain.PhotoStatus(*pstatus), CreatedAt: *pcreated,
				},
				Annotations: []domain.Annotation{},
			})
		}
		if aid != nil {
			cur := &detail.Photos[len(detail.Photos)-1]
			cur.Annotations = append(cur.Annotations, annotationFrom(alabel, atext, lname, lcol, larchived, *ax, *ay, *aw, *ah))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: read submission detail: %w", err)
	}
	if detail == nil {
		return nil, fmt.Errorf("submission %s: %w", id, domain.ErrNotFound)
	}
	return detail, nil
}

// annotationFrom menyusun anotasi dari kolom hasil LEFT JOIN labels: berlabel (label terkini) atau
// baris lama berteks bebas.
func annotationFrom(labelID *int, text, name, color *string, archivedAt *time.Time, x, y, w, h float64) domain.Annotation {
	a := domain.Annotation{X: x, Y: y, W: w, H: h}
	if labelID != nil && name != nil && color != nil {
		a.LabelID = *labelID
		a.Label = &domain.Label{ID: *labelID, Name: *name, Color: *color, ArchivedAt: archivedAt}
	}
	if text != nil {
		a.Text = *text
	}
	return a
}

func (r *SubmissionRepo) PhotoLabelIDs(ctx context.Context, photoIDs []uuid.UUID) (map[uuid.UUID]map[int]bool, error) {
	out := make(map[uuid.UUID]map[int]bool, len(photoIDs))
	if len(photoIDs) == 0 {
		return out, nil
	}
	rows, err := r.db(ctx).Query(ctx,
		`SELECT DISTINCT photo_id, label_id FROM annotations WHERE photo_id = ANY($1) AND label_id IS NOT NULL`, photoIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres: photo label ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			pid uuid.UUID
			lid int
		)
		if err := rows.Scan(&pid, &lid); err != nil {
			return nil, fmt.Errorf("postgres: scan photo label ids: %w", err)
		}
		if out[pid] == nil {
			out[pid] = map[int]bool{}
		}
		out[pid][lid] = true
	}
	return out, rows.Err()
}

func (r *SubmissionRepo) AddAnnotations(ctx context.Context, photoID uuid.UUID, anns []domain.Annotation) error {
	if len(anns) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, a := range anns {
		// baris baru selalu label_id; text hanya untuk baris lama (tidak pernah ditulis lagi)
		batch.Queue(`INSERT INTO annotations (photo_id, label_id, x, y, w, h) VALUES ($1,$2,$3,$4,$5,$6)`,
			photoID, a.LabelID, a.X, a.Y, a.W, a.H)
	}
	res := r.db(ctx).SendBatch(ctx, batch)
	var firstErr error
	for range anns {
		if _, err := res.Exec(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := res.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if firstErr != nil {
		return fmt.Errorf("postgres: insert annotations: %w", firstErr)
	}
	return nil
}

func (r *SubmissionRepo) ReplaceAnnotations(ctx context.Context, photoID uuid.UUID, anns []domain.Annotation) error {
	// hapus lalu tambah harus atomik; WithTx bergabung bila pemanggil sudah dalam transaksi
	return r.tx.WithTx(ctx, func(ctx context.Context) error {
		if _, err := r.db(ctx).Exec(ctx, `DELETE FROM annotations WHERE photo_id = $1`, photoID); err != nil {
			return fmt.Errorf("postgres: delete annotations: %w", err)
		}
		return r.AddAnnotations(ctx, photoID, anns)
	})
}

func (r *SubmissionRepo) BumpVersion(ctx context.Context, id uuid.UUID, expected int, now time.Time) (int, error) {
	var v int
	err := r.db(ctx).QueryRow(ctx,
		`UPDATE submissions SET version = version + 1, updated_at = $3 WHERE id = $1 AND version = $2 AND deleted_at IS NULL RETURNING version`,
		id, expected, now).Scan(&v)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("postgres: bump version: %w", err)
	}
	var exists bool
	if qerr := r.db(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM submissions WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&exists); qerr != nil {
		return 0, fmt.Errorf("postgres: bump version: %w", qerr)
	}
	if !exists {
		return 0, fmt.Errorf("submission %s: %w", id, domain.ErrNotFound)
	}
	return 0, domain.Conflict("VERSION_CONFLICT", "the submission was changed by someone else; reload and try again")
}

var _ = netip.Addr{} // ClientIP *netip.Addr dipetakan ke kolom inet oleh pgx
