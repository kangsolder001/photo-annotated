package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// Status hasil hapus permanen.
const (
	DeleteStatusDeleted        = "deleted"         // semua file dan baris sudah terhapus
	DeleteStatusCleanupPending = "cleanup_pending" // data sudah tersembunyi; sebagian file menunggu cleanup
	DeleteStatusNotFound       = "not_found"       // hanya pada hapus massal
)

// MaxBulkDelete: jumlah id maksimal per hapus massal.
const MaxBulkDelete = 50

// filePhaseTimeout: batas waktu langkah 2 (hapus file + baris) setelah commit. Dijalankan lepas dari
// pembatalan request: klien yang memutus koneksi tidak menghentikan pembersihan yang sudah dimulai.
const filePhaseTimeout = 2 * time.Minute

// AdminDeleteResult adalah hasil satu hapus permanen.
type AdminDeleteResult struct {
	SubmissionID       uuid.UUID
	Status             string
	PhotosDeleted      int
	AnnotationsDeleted int
	SubmissionDeleted  bool
	FilesDeleted       int
	FileFailures       int
}

// AdminDelete menghapus permanen submission atau foto (spec-labels-admin §5.5) dengan urutan cleanup
// induk: FILE DULU, BARIS KEMUDIAN. Langkah 1 (satu transaksi) menyembunyikan data dan menghapus
// anotasi; langkah 2 (setelah commit) menghapus file lalu barisnya. Baris yang filenya gagal dihapus
// tetap berstatus deleted dan diselesaikan cleanup berkala, sehingga tidak pernah ada baris yang
// menunjuk file yang sudah hilang.
type AdminDelete struct {
	repo  DeletionRepo
	tx    TxManager
	store PhotoStore
	clock Clock
}

func NewAdminDelete(repo DeletionRepo, tx TxManager, store PhotoStore, clock Clock) *AdminDelete {
	return &AdminDelete{repo: repo, tx: tx, store: store, clock: clock}
}

// DeleteSubmission menghapus submission beserta semua foto, anotasi, dan filenya.
func (u *AdminDelete) DeleteSubmission(ctx context.Context, id uuid.UUID) (*AdminDeleteResult, error) {
	return u.run(ctx, func(ctx context.Context, now time.Time) (*DeletionMark, error) {
		return u.repo.MarkSubmission(ctx, id, now)
	})
}

// DeletePhoto menghapus satu foto attached; foto terakhir ikut menghapus submission-nya.
func (u *AdminDelete) DeletePhoto(ctx context.Context, photoID uuid.UUID) (*AdminDeleteResult, error) {
	return u.run(ctx, func(ctx context.Context, now time.Time) (*DeletionMark, error) {
		return u.repo.MarkPhoto(ctx, photoID, now)
	})
}

// ValidateBulkIDs: 1–50 id, tanpa duplikat.
func ValidateBulkIDs(ids []uuid.UUID) error {
	if len(ids) < 1 || len(ids) > MaxBulkDelete {
		return domain.Invalid("INVALID_IDS", "ids must contain between 1 and %d submission ids", MaxBulkDelete)
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			return domain.Invalid("INVALID_IDS", "ids must be distinct submission ids")
		}
		seen[id] = true
	}
	return nil
}

// DeleteMany menghapus beberapa submission; diproses satu per satu (transaksi masing-masing). Id
// yang tidak ada atau sudah dihapus dilaporkan not_found. Error lain menghentikan proses.
func (u *AdminDelete) DeleteMany(ctx context.Context, ids []uuid.UUID) ([]AdminDeleteResult, error) {
	if err := ValidateBulkIDs(ids); err != nil {
		return nil, err
	}
	out := make([]AdminDeleteResult, 0, len(ids))
	for _, id := range ids {
		res, err := u.DeleteSubmission(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			out = append(out, AdminDeleteResult{SubmissionID: id, Status: DeleteStatusNotFound})
			continue
		}
		if err != nil {
			return out, err
		}
		out = append(out, *res)
	}
	return out, nil
}

func (u *AdminDelete) run(ctx context.Context, mark func(context.Context, time.Time) (*DeletionMark, error)) (*AdminDeleteResult, error) {
	var m *DeletionMark
	err := u.tx.WithTx(ctx, func(ctx context.Context) error {
		var err error
		m, err = mark(ctx, u.clock.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	res := &AdminDeleteResult{SubmissionID: m.SubmissionID, PhotosDeleted: m.PhotosDeleted, AnnotationsDeleted: m.AnnotationsDeleted,
		SubmissionDeleted: m.SubmissionDeleted, Status: DeleteStatusDeleted}
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), filePhaseTimeout)
	defer cancel()
	u.removeFiles(fctx, m, res)
	return res, nil
}

// removeFiles menjalankan langkah 2: hapus file (idempoten) lalu barisnya; terakhir baris submission
// bila sudah kosong. Kegagalan apa pun membuat status cleanup_pending; sisanya dikerjakan cleanup.
func (u *AdminDelete) removeFiles(ctx context.Context, m *DeletionMark, res *AdminDeleteResult) {
	for _, f := range m.Files {
		if err := u.store.Delete(ctx, f.StorageKey); err != nil && !errors.Is(err, ErrInvalidStorageKey) {
			res.FileFailures++
			continue // baris dipertahankan (deleted, tidak terlihat) untuk cleanup
		}
		res.FilesDeleted++
		if err := u.repo.DeletePhotoRow(ctx, f.ID); err != nil {
			res.FileFailures++
		}
	}
	if m.SubmissionDeleted {
		if gone, err := u.repo.DeleteSubmissionIfEmpty(ctx, m.SubmissionID); err != nil || !gone {
			res.FileFailures++
		}
	}
	if res.FileFailures > 0 {
		res.Status = DeleteStatusCleanupPending
	}
}
