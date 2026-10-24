package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	defaultMaxBatches = 50  // batas putaran per langkah dalam satu run; sisa dikerjakan run berikutnya
	orphanChunk       = 200 // jumlah kunci yang dicek ke DB sekaligus saat menyapu file yatim
	maxReportedErrors = 5
)

// CleanupConfig adalah parameter job pembersihan (dari config retention.*).
type CleanupConfig struct {
	RetentionDays int           // submission lebih tua dari ini dihapus beserta fotonya
	PendingTTL    time.Duration // foto pending yang tak pernah dipakai dibuang setelah ini
	Batch         int           // ukuran satu putaran kueri
	MaxBatches    int           // 0 = default
	TmpMaxAge     time.Duration // sisa tmp/*.part lebih tua dari ini dihapus
	OrphanMinAge  time.Duration // file tanpa baris foto baru dihapus bila lebih tua dari ini
}

// CleanupStep adalah langkah tambahan yang dijalankan di akhir RunOnce (mis. data auth).
type CleanupStep interface {
	Name() string
	Run(ctx context.Context, now time.Time) (deleted int, err error)
}

// CleanupReport meringkas satu putaran RunOnce.
type CleanupReport struct {
	StalePendingClaimed int // foto pending basi yang di-claim
	RetentionPhotos     int // foto yang ditandai karena submission-nya melewati retensi
	FilesDeleted        int
	FileDeleteFailures  int
	PhotoRowsDeleted    int
	SubmissionsDeleted  int
	TmpSwept            int
	Steps               map[string]int
	Duration            time.Duration
}

// Empty true bila putaran ini tidak mengubah apa pun.
func (r CleanupReport) Empty() bool {
	n := r.StalePendingClaimed + r.RetentionPhotos + r.FilesDeleted + r.FileDeleteFailures +
		r.PhotoRowsDeleted + r.SubmissionsDeleted + r.TmpSwept
	for _, v := range r.Steps {
		n += v
	}
	return n == 0
}

// OrphanReport meringkas sapuan file yatim.
type OrphanReport struct {
	Scanned  int // file lama yang diperiksa
	Removed  int // file yang tidak punya baris foto dan dihapus
	Failures int
	Duration time.Duration
}

// Cleanup menjalankan pembersihan foto, retensi, dan sapuan file yatim (spec §8.3). Aturan
// utamanya: FILE DULU, BARIS KEMUDIAN. Baris foto baru dihapus setelah filenya terhapus, dan baris
// submission baru dihapus setelah seluruh baris fotonya hilang, sehingga kegagalan menghapus file
// tidak pernah meninggalkan file tanpa catatan.
type Cleanup struct {
	repo  CleanupRepo
	store PhotoStore
	clock Clock
	cfg   CleanupConfig
	steps []CleanupStep
}

func NewCleanup(repo CleanupRepo, store PhotoStore, clock Clock, cfg CleanupConfig, steps ...CleanupStep) *Cleanup {
	if cfg.Batch < 1 {
		panic("usecase: CleanupConfig.Batch harus >= 1")
	}
	if cfg.MaxBatches < 1 {
		cfg.MaxBatches = defaultMaxBatches
	}
	return &Cleanup{repo: repo, store: store, clock: clock, cfg: cfg, steps: steps}
}

// RunOnce menjalankan satu putaran. Kegagalan satu langkah tidak menghentikan langkah lain;
// semua error dikumpulkan dan dikembalikan bersama laporan sebagian.
func (u *Cleanup) RunOnce(ctx context.Context) (CleanupReport, error) {
	start := u.clock.Now()
	now := start
	rep := CleanupReport{Steps: map[string]int{}}
	var errs errList
	// foto yang filenya gagal dihapus pada run ini: tidak dicoba lagi di tahap berikutnya pada
	// run yang sama (dan tidak dihitung dua kali); baris dipertahankan untuk run berikutnya.
	failed := map[uuid.UUID]bool{}

	// 1. Foto pending yang tak pernah dipakai: claim atomik (jadi deleted) agar tidak bisa lagi
	//    dilampirkan sebelum filenya dihapus.
	pendingCutoff := now.Add(-u.cfg.PendingTTL)
	for i := 0; i < u.cfg.MaxBatches; i++ {
		if err := ctx.Err(); err != nil {
			return u.finish(rep, start, errs.with(err))
		}
		n, err := u.repo.ClaimStalePending(ctx, pendingCutoff, u.cfg.Batch)
		if err != nil {
			errs.add(err)
			break
		}
		rep.StalePendingClaimed += n
		if n < u.cfg.Batch {
			break
		}
	}

	// 2. Hapus file + baris foto yang rejected/deleted (termasuk hasil claim di atas dan edit).
	u.deletePhotos(ctx, &rep, &errs, failed)

	// 3. Retensi: tandai foto submission kedaluwarsa, hapus filenya, lalu hapus submission yang
	//    sudah tidak punya baris foto. Diulang selama masih ada kemajuan.
	retentionCutoff := now.AddDate(0, 0, -u.cfg.RetentionDays)
	for i := 0; i < u.cfg.MaxBatches; i++ {
		if err := ctx.Err(); err != nil {
			return u.finish(rep, start, errs.with(err))
		}
		marked, err := u.repo.ExpireSubmissions(ctx, retentionCutoff, u.cfg.Batch)
		if err != nil {
			errs.add(err)
			break
		}
		rep.RetentionPhotos += marked
		u.deletePhotos(ctx, &rep, &errs, failed)
		gone, err := u.repo.DeleteEmptyExpiredSubmissions(ctx, retentionCutoff, u.cfg.Batch)
		if err != nil {
			errs.add(err)
			break
		}
		rep.SubmissionsDeleted += gone
		if marked == 0 && gone == 0 {
			break
		}
	}

	// 4. Langkah tambahan (data auth, dst.)
	for _, s := range u.steps {
		if err := ctx.Err(); err != nil {
			return u.finish(rep, start, errs.with(err))
		}
		n, err := s.Run(ctx, now)
		rep.Steps[s.Name()] += n
		if err != nil {
			errs.add(fmt.Errorf("%s: %w", s.Name(), err))
		}
	}

	// 5. Sisa upload sementara
	n, err := u.store.SweepTmp(ctx, now.Add(-u.cfg.TmpMaxAge))
	rep.TmpSwept = n
	if err != nil {
		errs.add(fmt.Errorf("sweep tmp: %w", err))
	}
	return u.finish(rep, start, errs)
}

func (u *Cleanup) finish(rep CleanupReport, start time.Time, errs errList) (CleanupReport, error) {
	rep.Duration = u.clock.Now().Sub(start)
	return rep, errs.err()
}

// deletePhotos menghapus file lalu baris untuk semua foto rejected/deleted. Setiap baris dikunjungi
// paling banyak sekali per panggilan (paginasi berdasarkan id), sehingga baris yang filenya gagal
// dihapus tidak membuat loop berputar; ia dicoba lagi pada run berikutnya.
func (u *Cleanup) deletePhotos(ctx context.Context, rep *CleanupReport, errs *errList, failed map[uuid.UUID]bool) {
	after := uuid.Nil
	for i := 0; i < u.cfg.MaxBatches; i++ {
		if ctx.Err() != nil {
			errs.add(ctx.Err())
			return
		}
		refs, err := u.repo.ListDeletablePhotos(ctx, after, u.cfg.Batch)
		if err != nil {
			errs.add(err)
			return
		}
		for _, ref := range refs {
			after = ref.ID
			if failed[ref.ID] {
				continue
			}
			if err := u.store.Delete(ctx, ref.StorageKey); err != nil && !errors.Is(err, ErrInvalidStorageKey) {
				failed[ref.ID] = true
				rep.FileDeleteFailures++
				errs.add(fmt.Errorf("delete file of photo %s: %w", ref.ID, err))
				continue // baris dipertahankan; dicoba lagi pada run berikutnya
			}
			if err == nil {
				rep.FilesDeleted++
			}
			if err := u.repo.DeletePhotoRow(ctx, ref.ID); err != nil {
				errs.add(err)
				continue
			}
			rep.PhotoRowsDeleted++
		}
		if len(refs) < u.cfg.Batch {
			return
		}
	}
}

// SweepOrphans menghapus file di SSD yang tidak punya baris foto sama sekali dan sudah lebih tua
// dari OrphanMinAge (mis. proses mati antara file terbit dan baris dicatat, atau upload yang
// kalah balapan dengan cleanup). Tidak ada file yang dihapus bila pengecekan ke DB gagal.
func (u *Cleanup) SweepOrphans(ctx context.Context) (OrphanReport, error) {
	start := u.clock.Now()
	var rep OrphanReport
	cutoff := start.Add(-u.cfg.OrphanMinAge)
	chunk := make([]string, 0, orphanChunk)

	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		existing, err := u.repo.ExistingStorageKeys(ctx, chunk)
		if err != nil {
			return err // jangan menghapus apa pun tanpa kepastian dari DB
		}
		for _, key := range chunk {
			if existing[key] {
				continue
			}
			if err := u.store.Delete(ctx, key); err != nil {
				rep.Failures++
				continue
			}
			rep.Removed++
		}
		chunk = chunk[:0]
		return nil
	}
	err := u.store.WalkOlderThan(ctx, cutoff, func(key string) error {
		rep.Scanned++
		chunk = append(chunk, key)
		if len(chunk) >= orphanChunk {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	rep.Duration = u.clock.Now().Sub(start)
	if err != nil {
		return rep, fmt.Errorf("sweep orphans: %w", err)
	}
	return rep, nil
}

// errList mengumpulkan error tanpa membanjiri log: hanya beberapa yang pertama disimpan.
type errList struct {
	errs  []error
	total int
}

func (l *errList) add(err error) {
	l.total++
	if len(l.errs) < maxReportedErrors {
		l.errs = append(l.errs, err)
	}
}

func (l errList) with(err error) errList {
	l.add(err)
	return l
}

func (l errList) err() error {
	if l.total == 0 {
		return nil
	}
	e := errors.Join(l.errs...)
	if l.total > len(l.errs) {
		return fmt.Errorf("%w (+%d error lain)", e, l.total-len(l.errs))
	}
	return e
}
