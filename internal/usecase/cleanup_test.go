package usecase_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/imgfix"
	"photo-annotated/internal/usecase"
)

type cleanRig struct {
	*subRig
	cu    *usecase.Cleanup
	repo  *postgres.CleanupRepo
	cfg   usecase.CleanupConfig
	store usecase.PhotoStore
}

type cleanOpts struct {
	retentionOff bool // RetentionDays = 0 (retensi mati)
	batch        int
	maxBatches   int
	store        func(usecase.PhotoStore) usecase.PhotoStore
	repo         func(usecase.CleanupRepo) usecase.CleanupRepo
	steps        []usecase.CleanupStep
}

func newCleanRig(t *testing.T, o cleanOpts) *cleanRig {
	t.Helper()
	base := newSubRig(t, rigOpts{})
	if o.batch == 0 {
		o.batch = 100
	}
	cfg := usecase.CleanupConfig{
		RetentionDays: 90, PendingTTL: 24 * time.Hour, Batch: o.batch, MaxBatches: o.maxBatches,
		TmpMaxAge: time.Hour, OrphanMinAge: 24 * time.Hour,
	}
	if o.retentionOff {
		cfg.RetentionDays = 0
	}
	repo := postgres.NewCleanupRepo(base.pool)
	var cr usecase.CleanupRepo = repo
	if o.repo != nil {
		cr = o.repo(cr)
	}
	var st usecase.PhotoStore = base.store
	if o.store != nil {
		st = o.store(st)
	}
	r := &cleanRig{subRig: base, repo: repo, cfg: cfg, store: st}
	r.cu = usecase.NewCleanup(cr, st, base.clock, cfg, o.steps...)
	return r
}

func (r *cleanRig) fileExists(p *domain.Photo) bool {
	_, err := os.Stat(filepath.Join(r.dir, p.StorageKey))
	return err == nil
}

func (r *cleanRig) rowExists(t *testing.T, p *domain.Photo) bool {
	t.Helper()
	_, err := r.photos.Get(bg, p.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func (r *cleanRig) setPhotoAge(t *testing.T, p *domain.Photo, age time.Duration) {
	t.Helper()
	if _, err := r.pool.Exec(bg, `UPDATE photos SET created_at=$2 WHERE id=$1`, p.ID, r.clock.t.Add(-age)); err != nil {
		t.Fatal(err)
	}
}

func (r *cleanRig) setStatus(t *testing.T, p *domain.Photo, st domain.PhotoStatus) {
	t.Helper()
	if _, err := r.pool.Exec(bg, `UPDATE photos SET status=$2 WHERE id=$1`, p.ID, string(st)); err != nil {
		t.Fatal(err)
	}
}

func (r *cleanRig) setSubmissionAge(t *testing.T, id uuid.UUID, age time.Duration) {
	t.Helper()
	if _, err := r.pool.Exec(bg, `UPDATE submissions SET created_at=$2 WHERE id=$1`, id, r.clock.t.Add(-age)); err != nil {
		t.Fatal(err)
	}
}

func (r *cleanRig) submissionExists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	_, err := r.subs.Get(bg, id)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// attachedSubmission membuat submission nyata (n foto, masing-masing 1 anotasi) lalu menuakannya.
func (r *cleanRig) attachedSubmission(t *testing.T, n int, age time.Duration) (uuid.UUID, []*domain.Photo) {
	t.Helper()
	var ps []*domain.Photo
	for i := 0; i < n; i++ {
		ps = append(ps, r.uploaded(t, nil))
	}
	out, err := r.uc.Submit(bg, in(ps...))
	if err != nil {
		t.Fatal(err)
	}
	r.setSubmissionAge(t, out.SubmissionID, age)
	return out.SubmissionID, ps
}

func mustRun(t *testing.T, r *cleanRig) usecase.CleanupReport {
	t.Helper()
	rep, err := r.cu.RunOnce(bg)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	return rep
}

// ---------------------------------------------------------------- foto pending basi & rejected/deleted

func TestCleanupRemovesStalePendingButKeepsYoungAndAttached(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	stale := r.uploaded(t, nil)
	r.setPhotoAge(t, stale, 25*time.Hour)
	atBoundary := r.uploaded(t, nil)
	r.setPhotoAge(t, atBoundary, 24*time.Hour) // tepat pada TTL: belum basi
	young := r.uploaded(t, nil)
	r.setPhotoAge(t, young, time.Hour)
	_, attached := r.attachedSubmission(t, 1, time.Hour)
	r.setPhotoAge(t, attached[0], 72*time.Hour) // umur foto tidak relevan bagi yang sudah attached

	rep := mustRun(t, r)
	if rep.StalePendingClaimed != 1 || rep.FilesDeleted != 1 || rep.PhotoRowsDeleted != 1 || rep.FileDeleteFailures != 0 {
		t.Fatalf("laporan: %+v", rep)
	}
	if r.fileExists(stale) || r.rowExists(t, stale) {
		t.Fatal("foto pending basi harus hilang (file dan baris)")
	}
	for name, p := range map[string]*domain.Photo{"tepat batas": atBoundary, "muda": young, "attached": attached[0]} {
		if !r.fileExists(p) || !r.rowExists(t, p) {
			t.Fatalf("foto %s tidak boleh tersentuh", name)
		}
	}
}

func TestCleanupRemovesRejectedAndDeletedImmediately(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	rej, del := r.uploaded(t, nil), r.uploaded(t, nil)
	r.setStatus(t, rej, domain.PhotoRejected)
	r.setStatus(t, del, domain.PhotoDeleted)
	rep := mustRun(t, r)
	if rep.FilesDeleted != 2 || rep.PhotoRowsDeleted != 2 {
		t.Fatalf("laporan: %+v", rep)
	}
	for _, p := range []*domain.Photo{rej, del} {
		if r.fileExists(p) || r.rowExists(t, p) {
			t.Fatal("harus hilang")
		}
	}
}

func TestCleanupRemovesPhotosRemovedByEdit(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	ed := newEditRigFrom(t, r.subRig)
	out, ps := r.submittedVia(t, ed, 2)
	if _, err := ed.ed.Edit(bg, usecase.EditInput{SubmissionID: out.SubmissionID, Caller: usecase.Caller{EditToken: out.EditToken}, Version: 1,
		Photos: []usecase.SubmitPhoto{ep(ps[0], "tetap")}}); err != nil {
		t.Fatal(err)
	}
	if !r.fileExists(ps[1]) {
		t.Fatal("file foto yang dibuang masih ada sampai cleanup")
	}
	mustRun(t, r)
	if r.fileExists(ps[1]) || r.rowExists(t, ps[1]) {
		t.Fatal("cleanup harus menghapus foto yang dibuang lewat edit")
	}
	if !r.fileExists(ps[0]) || !r.rowExists(t, ps[0]) {
		t.Fatal("foto yang dipertahankan tidak boleh tersentuh")
	}
}

func TestCleanupIsIdempotentAndSecondRunIsEmpty(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	p := r.uploaded(t, nil)
	r.setStatus(t, p, domain.PhotoRejected)
	if rep := mustRun(t, r); rep.Empty() {
		t.Fatal("putaran pertama harus mengerjakan sesuatu")
	}
	if rep := mustRun(t, r); !rep.Empty() {
		t.Fatalf("putaran kedua harus kosong: %+v", rep)
	}
}

func TestCleanupDeletesRowEvenWhenFileIsAlreadyGone(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	p := r.uploaded(t, nil)
	r.setStatus(t, p, domain.PhotoDeleted)
	if err := os.Remove(filepath.Join(r.dir, p.StorageKey)); err != nil {
		t.Fatal(err)
	}
	rep := mustRun(t, r)
	if rep.PhotoRowsDeleted != 1 || rep.FileDeleteFailures != 0 || r.rowExists(t, p) {
		t.Fatalf("file yang sudah tidak ada bukan kegagalan: %+v", rep)
	}
}

// ---------------------------------------------------------------- kegagalan hapus file

type failingDeleteStore struct {
	usecase.PhotoStore
	mu   sync.Mutex
	fail map[string]bool
}

func (f *failingDeleteStore) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	bad := f.fail[key]
	f.mu.Unlock()
	if bad {
		return domain.Unavailable("STORAGE_UNAVAILABLE", "photo storage is unavailable")
	}
	return f.PhotoStore.Delete(ctx, key)
}

func TestCleanupKeepsRowWhenFileDeletionFailsAndRetriesLater(t *testing.T) {
	var fs *failingDeleteStore
	r := newCleanRig(t, cleanOpts{store: func(s usecase.PhotoStore) usecase.PhotoStore {
		fs = &failingDeleteStore{PhotoStore: s, fail: map[string]bool{}}
		return fs
	}})
	a, b, c := r.uploaded(t, nil), r.uploaded(t, nil), r.uploaded(t, nil)
	for _, p := range []*domain.Photo{a, b, c} {
		r.setStatus(t, p, domain.PhotoRejected)
	}
	fs.fail[b.StorageKey] = true

	rep, err := r.cu.RunOnce(bg)
	if err == nil {
		t.Fatal("kegagalan hapus file harus dilaporkan sebagai error")
	}
	if rep.FileDeleteFailures != 1 || rep.FilesDeleted != 2 || rep.PhotoRowsDeleted != 2 {
		t.Fatalf("laporan: %+v", rep)
	}
	if !r.rowExists(t, b) || !r.fileExists(b) {
		t.Fatal("baris dan file yang gagal dihapus harus dipertahankan (file dulu, baris kemudian)")
	}
	if r.rowExists(t, a) || r.rowExists(t, c) || r.fileExists(a) || r.fileExists(c) {
		t.Fatal("foto lain tetap harus dibersihkan")
	}
	// penyimpanan pulih → putaran berikutnya menuntaskan
	fs.mu.Lock()
	fs.fail = map[string]bool{}
	fs.mu.Unlock()
	rep = mustRun(t, r)
	if rep.FilesDeleted != 1 || r.rowExists(t, b) || r.fileExists(b) {
		t.Fatalf("laporan: %+v", rep)
	}
}

func TestCleanupDoesNotSpinOnPermanentlyFailingRow(t *testing.T) {
	var fs *failingDeleteStore
	r := newCleanRig(t, cleanOpts{batch: 2, store: func(s usecase.PhotoStore) usecase.PhotoStore {
		fs = &failingDeleteStore{PhotoStore: s, fail: map[string]bool{}}
		return fs
	}})
	var all []*domain.Photo
	for i := 0; i < 5; i++ {
		p := r.uploaded(t, nil)
		r.setStatus(t, p, domain.PhotoRejected)
		all = append(all, p)
		fs.fail[p.StorageKey] = true
	}
	done := make(chan usecase.CleanupReport, 1)
	go func() { rep, _ := r.cu.RunOnce(bg); done <- rep }()
	select {
	case rep := <-done:
		if rep.FileDeleteFailures != 5 {
			t.Fatalf("setiap baris harus dicoba tepat sekali per run: %+v", rep)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunOnce berputar tanpa henti pada baris yang selalu gagal")
	}
}

// ---------------------------------------------------------------- retensi

func TestCleanupRetentionDeletesExpiredSubmissionsCompletely(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	oldID, oldPhotos := r.attachedSubmission(t, 2, 91*24*time.Hour)
	_, atPhotos := r.attachedSubmission(t, 1, 90*24*time.Hour) // tepat pada retensi: belum
	freshID, freshPhotos := r.attachedSubmission(t, 1, 89*24*time.Hour)

	rep := mustRun(t, r)
	if rep.RetentionPhotos != 2 || rep.SubmissionsDeleted != 1 || rep.FilesDeleted != 2 || rep.PhotoRowsDeleted != 2 {
		t.Fatalf("laporan: %+v", rep)
	}
	if r.submissionExists(t, oldID) {
		t.Fatal("submission kedaluwarsa harus terhapus")
	}
	for _, p := range oldPhotos {
		if r.fileExists(p) || r.rowExists(t, p) {
			t.Fatal("file dan baris foto submission kedaluwarsa harus hilang")
		}
	}
	var n int
	_ = r.pool.QueryRow(bg, `SELECT count(*) FROM annotations`).Scan(&n)
	if n != 2 { // hanya anotasi dari 2 submission yang tersisa
		t.Fatalf("anotasi tersisa = %d, want 2", n)
	}
	for _, p := range append(atPhotos, freshPhotos...) {
		if !r.fileExists(p) || !r.rowExists(t, p) {
			t.Fatal("submission yang belum kedaluwarsa tidak boleh tersentuh")
		}
	}
	if !r.submissionExists(t, freshID) {
		t.Fatal("submission baru tidak boleh terhapus")
	}
}

// spec-pendataan §8: retention.days = 0 → submission setua apa pun tidak terhapus, tetapi foto
// pending basi tetap dibersihkan.
func TestCleanupRetentionDisabledKeepsOldSubmissions(t *testing.T) {
	r := newCleanRig(t, cleanOpts{retentionOff: true})
	oldID, oldPhotos := r.attachedSubmission(t, 2, 400*24*time.Hour)
	empty := &domain.Submission{ID: uuid.New(), ClientRequestID: uuid.New(), Version: 1, CreatedAt: r.clock.t.Add(-200 * 24 * time.Hour), UpdatedAt: r.clock.t}
	if err := r.subs.Create(bg, empty); err != nil {
		t.Fatal(err)
	}
	stale := r.uploaded(t, nil)
	r.setPhotoAge(t, stale, 25*time.Hour)

	rep := mustRun(t, r)
	if rep.RetentionPhotos != 0 || rep.SubmissionsDeleted != 0 || rep.StalePendingClaimed != 1 || rep.FilesDeleted != 1 {
		t.Fatalf("laporan: %+v", rep)
	}
	if !r.submissionExists(t, oldID) || !r.submissionExists(t, empty.ID) {
		t.Fatal("submission lama tidak boleh terhapus saat retensi mati")
	}
	for _, p := range oldPhotos {
		if !r.fileExists(p) || !r.rowExists(t, p) {
			t.Fatal("foto submission lama tidak boleh tersentuh")
		}
	}
	if r.fileExists(stale) || r.rowExists(t, stale) {
		t.Fatal("foto pending basi tetap harus dibersihkan")
	}
}

func TestCleanupRetentionWaitsWhenFileDeletionFails(t *testing.T) {
	var fs *failingDeleteStore
	r := newCleanRig(t, cleanOpts{store: func(s usecase.PhotoStore) usecase.PhotoStore {
		fs = &failingDeleteStore{PhotoStore: s, fail: map[string]bool{}}
		return fs
	}})
	id, ps := r.attachedSubmission(t, 2, 100*24*time.Hour)
	fs.fail[ps[1].StorageKey] = true

	rep, err := r.cu.RunOnce(bg)
	if err == nil || rep.FileDeleteFailures != 1 {
		t.Fatalf("err=%v rep=%+v", err, rep)
	}
	// submission dan satu baris foto bertahan: file dihapus dulu, baru baris
	if !r.submissionExists(t, id) || !r.rowExists(t, ps[1]) || !r.fileExists(ps[1]) {
		t.Fatal("submission/baris/file yang gagal dihapus harus dipertahankan")
	}
	if r.fileExists(ps[0]) || r.rowExists(t, ps[0]) {
		t.Fatal("foto yang berhasil dihapus harus hilang")
	}
	// fotonya sudah tak terlihat klien (deleted)
	if d, err := r.subs.GetDetail(bg, id); err != nil || len(d.Photos) != 0 {
		t.Fatalf("foto kedaluwarsa tidak boleh terlihat: %+v %v", d, err)
	}
	fs.mu.Lock()
	fs.fail = map[string]bool{}
	fs.mu.Unlock()
	mustRun(t, r)
	if r.submissionExists(t, id) || r.fileExists(ps[1]) {
		t.Fatal("setelah penyimpanan pulih semuanya harus bersih")
	}
}

func TestCleanupDeletesExpiredSubmissionWithoutPhotos(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	s := &domain.Submission{ID: uuid.New(), ClientRequestID: uuid.New(), Version: 1, CreatedAt: r.clock.t.Add(-200 * 24 * time.Hour), UpdatedAt: r.clock.t}
	if err := r.subs.Create(bg, s); err != nil {
		t.Fatal(err)
	}
	rep := mustRun(t, r)
	if rep.SubmissionsDeleted != 1 || r.submissionExists(t, s.ID) {
		t.Fatalf("laporan: %+v", rep)
	}
}

// ---------------------------------------------------------------- batch & batas run

func TestCleanupProcessesAllItemsAcrossBatches(t *testing.T) {
	r := newCleanRig(t, cleanOpts{batch: 2})
	var ps []*domain.Photo
	for i := 0; i < 7; i++ {
		p := r.uploaded(t, nil)
		r.setStatus(t, p, domain.PhotoDeleted)
		ps = append(ps, p)
	}
	var subs []uuid.UUID
	for i := 0; i < 5; i++ {
		id, _ := r.attachedSubmission(t, 1, 100*24*time.Hour)
		subs = append(subs, id)
	}
	rep := mustRun(t, r)
	if rep.PhotoRowsDeleted != 12 || rep.SubmissionsDeleted != 5 {
		t.Fatalf("laporan: %+v", rep)
	}
	for _, p := range ps {
		if r.rowExists(t, p) || r.fileExists(p) {
			t.Fatal("semua foto harus hilang")
		}
	}
	for _, id := range subs {
		if r.submissionExists(t, id) {
			t.Fatal("semua submission kedaluwarsa harus hilang")
		}
	}
}

func TestCleanupRunIsBoundedByMaxBatches(t *testing.T) {
	r := newCleanRig(t, cleanOpts{batch: 2, maxBatches: 1})
	for i := 0; i < 7; i++ {
		p := r.uploaded(t, nil)
		r.setStatus(t, p, domain.PhotoDeleted)
	}
	counts := []int{}
	for i := 0; i < 5; i++ {
		rep := mustRun(t, r)
		counts = append(counts, rep.PhotoRowsDeleted)
		if rep.PhotoRowsDeleted > 2+2 { // paling banyak satu batch dari tiap tahap
			t.Fatalf("satu run melewati batas: %+v", rep)
		}
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if total != 7 {
		t.Fatalf("seluruh antrean harus tuntas lewat beberapa run: %v", counts)
	}
}

// ---------------------------------------------------------------- tmp, langkah tambahan, konteks

type stubStep struct {
	name string
	n    int
	err  error
	ran  int
	when time.Time
}

func (s *stubStep) Name() string { return s.name }
func (s *stubStep) Run(_ context.Context, now time.Time) (int, error) {
	s.ran++
	s.when = now
	return s.n, s.err
}

func TestCleanupRunsExtraStepsAndTheirErrorsDoNotStopOthers(t *testing.T) {
	ok := &stubStep{name: "auth", n: 3}
	bad := &stubStep{name: "rusak", err: errors.New("langkah gagal")}
	r := newCleanRig(t, cleanOpts{steps: []usecase.CleanupStep{bad, ok}})
	p := r.uploaded(t, nil)
	r.setStatus(t, p, domain.PhotoRejected)

	rep, err := r.cu.RunOnce(bg)
	if err == nil || !errors.Is(err, bad.err) {
		t.Fatalf("err = %v", err)
	}
	if ok.ran != 1 || bad.ran != 1 || rep.Steps["auth"] != 3 || !ok.when.Equal(r.clock.t) {
		t.Fatalf("langkah: ok=%+v bad=%+v rep=%+v", ok, bad, rep)
	}
	if r.rowExists(t, p) {
		t.Fatal("pembersihan foto tetap harus berjalan")
	}
	if rep.Empty() {
		t.Fatal("laporan tidak kosong")
	}
}

func TestCleanupSweepsStaleTmpFiles(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	old := filepath.Join(r.dir, "tmp", "a.webp.1.part")
	young := filepath.Join(r.dir, "tmp", "b.webp.2.part")
	for _, p := range []string{old, young} {
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	past := r.clock.t.Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	// jam fake (2026-10-24 12:00 UTC) vs mtime nyata: jadikan keduanya relatif terhadap jam fake
	recent := r.clock.t.Add(-5 * time.Minute)
	if err := os.Chtimes(young, recent, recent); err != nil {
		t.Fatal(err)
	}
	rep := mustRun(t, r)
	if rep.TmpSwept != 1 {
		t.Fatalf("laporan: %+v", rep)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("tmp lama harus dihapus")
	}
	if _, err := os.Stat(young); err != nil {
		t.Fatal("tmp muda harus tetap ada")
	}
}

func TestCleanupHonorsCanceledContext(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	p := r.uploaded(t, nil)
	r.setStatus(t, p, domain.PhotoRejected)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, err := r.cu.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if !r.rowExists(t, p) {
		t.Fatal("konteks batal tidak boleh menghapus apa pun")
	}
}

func TestCleanupConcurrentRunsAreSafe(t *testing.T) {
	r := newCleanRig(t, cleanOpts{batch: 3})
	var ps []*domain.Photo
	for i := 0; i < 12; i++ {
		p := r.uploaded(t, nil)
		r.setStatus(t, p, domain.PhotoRejected)
		ps = append(ps, p)
	}
	for i := 0; i < 3; i++ {
		r.attachedSubmission(t, 1, 100*24*time.Hour)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = r.cu.RunOnce(bg)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("run %d: %v (Delete dan DeletePhotoRow harus idempoten)", i, err)
		}
	}
	mustRun(t, r) // tuntaskan sisa bila ada
	for _, p := range ps {
		if r.rowExists(t, p) || r.fileExists(p) {
			t.Fatal("semua harus bersih")
		}
	}
	var n int
	_ = r.pool.QueryRow(bg, `SELECT count(*) FROM submissions`).Scan(&n)
	if n != 0 {
		t.Fatalf("submission tersisa = %d", n)
	}
}

// ---------------------------------------------------------------- file yatim

func (r *cleanRig) orphanFile(t *testing.T, age time.Duration) string {
	t.Helper()
	id := uuid.New()
	key, _ := domain.NewStorageKey(id, r.clock.t, domain.ContentTypeWebP)
	p := filepath.Join(r.dir, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, imgfix.WebP(10, 10, 100, 0), 0o640); err != nil {
		t.Fatal(err)
	}
	mt := r.clock.t.Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSweepOrphansRemovesOnlyOldFilesWithoutRows(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	oldOrphan := r.orphanFile(t, 48*time.Hour)
	youngOrphan := r.orphanFile(t, time.Hour) // mungkin sedang di antara commit file dan catatan DB
	withRow := r.uploaded(t, nil)
	old := r.clock.t.Add(-72 * time.Hour)
	_ = os.Chtimes(filepath.Join(r.dir, withRow.StorageKey), old, old)
	deletedRow := r.uploaded(t, nil)
	r.setStatus(t, deletedRow, domain.PhotoDeleted) // status apa pun dihitung "punya baris"
	_ = os.Chtimes(filepath.Join(r.dir, deletedRow.StorageKey), old, old)
	foreign := filepath.Join(r.dir, "2026", "10", "catatan-admin.txt")
	_ = os.MkdirAll(filepath.Dir(foreign), 0o750)
	_ = os.WriteFile(foreign, []byte("jangan dihapus"), 0o640)
	_ = os.Chtimes(foreign, old, old)

	rep, err := r.cu.SweepOrphans(bg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Removed != 1 || rep.Scanned != 3 || rep.Failures != 0 {
		t.Fatalf("laporan: %+v (3 file lama diperiksa: yatim, ber-baris, ber-baris deleted)", rep)
	}
	if _, err := os.Stat(oldOrphan); !os.IsNotExist(err) {
		t.Fatal("file yatim lama harus dihapus")
	}
	for name, p := range map[string]string{
		"yatim muda": youngOrphan, "dengan baris": filepath.Join(r.dir, withRow.StorageKey),
		"baris deleted": filepath.Join(r.dir, deletedRow.StorageKey), "berkas asing": foreign,
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s tidak boleh dihapus: %v", name, err)
		}
	}
	// idempotent
	if rep, _ := r.cu.SweepOrphans(bg); rep.Removed != 0 {
		t.Fatalf("putaran kedua: %+v", rep)
	}
}

func TestSweepOrphansHandlesMoreFilesThanOneChunk(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	const n = 450 // > 2 chunk (200)
	paths := make([]string, n)
	for i := range paths {
		paths[i] = r.orphanFile(t, 48*time.Hour)
	}
	keep := r.uploaded(t, nil)
	_ = os.Chtimes(filepath.Join(r.dir, keep.StorageKey), r.clock.t.Add(-72*time.Hour), r.clock.t.Add(-72*time.Hour))
	rep, err := r.cu.SweepOrphans(bg)
	if err != nil || rep.Removed != n || rep.Scanned != n+1 {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, keep.StorageKey)); err != nil {
		t.Fatal("file ber-baris tidak boleh dihapus")
	}
}

type failingExistsRepo struct {
	usecase.CleanupRepo
	err error
}

func (f failingExistsRepo) ExistingStorageKeys(context.Context, []string) (map[string]bool, error) {
	return nil, f.err
}

func TestSweepOrphansDeletesNothingWhenDatabaseCheckFails(t *testing.T) {
	boom := errors.New("db mati")
	r := newCleanRig(t, cleanOpts{repo: func(c usecase.CleanupRepo) usecase.CleanupRepo { return failingExistsRepo{c, boom} }})
	// file yang SEBENARNYA punya baris: bila DB gagal, jangan sampai file ini dianggap yatim
	real := r.uploaded(t, nil)
	_ = os.Chtimes(filepath.Join(r.dir, real.StorageKey), r.clock.t.Add(-72*time.Hour), r.clock.t.Add(-72*time.Hour))
	orphan := r.orphanFile(t, 72*time.Hour)

	_, err := r.cu.SweepOrphans(bg)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	for _, p := range []string{filepath.Join(r.dir, real.StorageKey), orphan} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("tidak ada file yang boleh dihapus tanpa kepastian dari DB: %v", err)
		}
	}
}

func TestSweepOrphansReportsDeleteFailuresAndContinues(t *testing.T) {
	var fs *failingDeleteStore
	r := newCleanRig(t, cleanOpts{store: func(s usecase.PhotoStore) usecase.PhotoStore {
		fs = &failingDeleteStore{PhotoStore: s, fail: map[string]bool{}}
		return fs
	}})
	bad, good := r.orphanFile(t, 48*time.Hour), r.orphanFile(t, 48*time.Hour)
	rel, _ := filepath.Rel(r.dir, bad)
	fs.fail[rel] = true
	rep, err := r.cu.SweepOrphans(bg)
	if err != nil || rep.Removed != 1 || rep.Failures != 1 {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Fatal("file yatim lain tetap harus dihapus")
	}
}

func TestSweepOrphansHonorsContext(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	orphan := r.orphanFile(t, 48*time.Hour)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := r.cu.SweepOrphans(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatal("konteks batal tidak boleh menghapus")
	}
}

func TestNewCleanupPanicsOnInvalidBatch(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Batch 0 harus panic")
		}
	}()
	usecase.NewCleanup(nil, nil, usecase.SystemClock{}, usecase.CleanupConfig{})
}

// newEditRigFrom membangun usecase Edit di atas rig yang sama (DB, disk, dan jam yang sama).
func newEditRigFrom(t *testing.T, base *subRig) *editRig {
	t.Helper()
	rd := &readRig{subRig: base}
	return &editRig{readRig: rd, ed: usecase.NewEdit(base.photos, base.subs, base.labels, postgres.NewTxManager(base.pool), base.store, base.edit, base.clock,
		usecase.EditConfig{SubmitConfig: usecase.SubmitConfig{MaxPhotos: 5, MaxAnnotationsPerPhoto: 50}, Window: 24 * time.Hour})}
}

func (r *cleanRig) submittedVia(t *testing.T, ed *editRig, n int) (*usecase.SubmitOutput, []*domain.Photo) {
	t.Helper()
	var ps []*domain.Photo
	for i := 0; i < n; i++ {
		ps = append(ps, r.uploaded(t, nil))
	}
	out, err := r.uc.Submit(bg, in(ps...))
	if err != nil {
		t.Fatal(err)
	}
	return out, ps
}

// AC 17: submission accepted berumur > 90 hari tidak terhapus; pending/rejected seumur itu terhapus;
// membatalkan penerimaan membuatnya terhapus pada putaran berikutnya.
func TestCleanupRetentionSkipsAccepted(t *testing.T) {
	r := newCleanRig(t, cleanOpts{})
	accepted, accPhotos := r.attachedSubmission(t, 1, 120*24*time.Hour)
	rejected, _ := r.attachedSubmission(t, 1, 120*24*time.Hour)
	pending, _ := r.attachedSubmission(t, 1, 120*24*time.Hour)
	setReview := func(id uuid.UUID, status, reason string) {
		t.Helper()
		var rsn any
		if reason != "" {
			rsn = reason
		}
		var at any = "now()"
		if status == "pending" {
			at = nil
		}
		if _, err := r.pool.Exec(bg, `UPDATE submissions SET review_status = $2, review_reason = $3,
			reviewed_at = CASE WHEN $4::text IS NULL THEN NULL ELSE now() END WHERE id = $1`, id, status, rsn, at); err != nil {
			t.Fatal(err)
		}
	}
	setReview(accepted, "accepted", "")
	setReview(rejected, "rejected", "buruk")

	mustRun(t, r)
	if !r.submissionExists(t, accepted) || !r.fileExists(accPhotos[0]) {
		t.Fatal("submission accepted tidak boleh terhapus retensi")
	}
	if r.submissionExists(t, rejected) || r.submissionExists(t, pending) {
		t.Fatal("submission rejected/pending lewat retensi harus terhapus")
	}
	// batal diterima → terhapus pada putaran berikutnya
	setReview(accepted, "pending", "")
	mustRun(t, r)
	if r.submissionExists(t, accepted) || r.fileExists(accPhotos[0]) {
		t.Fatal("submission yang batal diterima harus terhapus pada putaran berikutnya")
	}
}
