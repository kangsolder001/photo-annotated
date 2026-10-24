package usecase_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type deleteRig struct {
	*editRig
	del   *usecase.AdminDelete
	fails *failingDeleteStore
}

func newDeleteRig(t *testing.T) *deleteRig {
	t.Helper()
	er := newEditRig(t, false)
	fs := &failingDeleteStore{PhotoStore: er.store, fail: map[string]bool{}}
	return &deleteRig{editRig: er, fails: fs,
		del: usecase.NewAdminDelete(postgres.NewDeletionRepo(er.pool), postgres.NewTxManager(er.pool), fs, er.clock)}
}

func (r *deleteRig) fileExists(p *domain.Photo) bool {
	_, err := os.Stat(filepath.Join(r.dir, p.StorageKey))
	return err == nil
}

func (r *deleteRig) rows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := r.pool.QueryRow(bg, sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdminDeleteSubmissionRemovesRowsAndFiles(t *testing.T) {
	r := newDeleteRig(t)
	out, p := r.submitted(t, nil, 3) // foto 0 punya 2 kotak, foto lain masing-masing 1
	other, _ := r.submitted(t, nil, 1)
	res, err := r.del.DeleteSubmission(bg, out.SubmissionID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != usecase.DeleteStatusDeleted || res.PhotosDeleted != 3 || res.AnnotationsDeleted != 4 || !res.SubmissionDeleted ||
		res.FilesDeleted != 3 || res.FileFailures != 0 {
		t.Fatalf("hasil = %+v", res)
	}
	for _, ph := range p {
		if r.fileExists(ph) {
			t.Fatal("file foto harus hilang dari SSD")
		}
	}
	if n := r.rows(t, `SELECT count(*) FROM submissions WHERE id = $1`, out.SubmissionID); n != 0 {
		t.Fatal("baris submission harus hilang")
	}
	if n := r.rows(t, `SELECT count(*) FROM photos WHERE submission_id = $1`, out.SubmissionID); n != 0 {
		t.Fatal("baris foto harus hilang")
	}
	// pemilik mendapat 404; submission lain utuh
	if _, err := r.rd.Submission(bg, usecase.Caller{EditToken: out.EditToken}, out.SubmissionID); errCode(err) != "NOT_FOUND" {
		t.Fatalf("baca setelah hapus: %v", err)
	}
	if _, err := r.rd.Submission(bg, usecase.Caller{EditToken: other.EditToken}, other.SubmissionID); err != nil {
		t.Fatalf("submission lain: %v", err)
	}
	if _, err := r.del.DeleteSubmission(bg, out.SubmissionID); errCode(err) != "NOT_FOUND" {
		t.Fatalf("hapus ulang: %v", err)
	}
	if _, err := r.del.DeleteSubmission(bg, uuid.New()); errCode(err) != "NOT_FOUND" {
		t.Fatalf("id tak ada: %v", err)
	}
	// retry POST dengan client_request_id yang sama membuat submission baru (kunci idempotensi ikut terhapus)
}

func TestAdminDeletePhotoCompactsAndBumpsVersion(t *testing.T) {
	r := newDeleteRig(t)
	out, p := r.submitted(t, nil, 3)
	r.clock.t = r.clock.t.Add(time.Hour)
	res, err := r.del.DeletePhoto(bg, p[1].ID)
	if err != nil || res.Status != usecase.DeleteStatusDeleted || res.PhotosDeleted != 1 || res.SubmissionDeleted {
		t.Fatalf("hapus foto: %+v %v", res, err)
	}
	if r.fileExists(p[1]) || !r.fileExists(p[0]) {
		t.Fatal("hanya file foto yang dihapus yang hilang")
	}
	d := r.detail(t, out.SubmissionID)
	if d.Submission.Version != 2 || !d.Submission.UpdatedAt.Equal(r.clock.t) || len(d.Photos) != 2 ||
		d.Photos[0].Photo.ID != p[0].ID || d.Photos[1].Photo.ID != p[2].ID || d.Photos[1].Photo.Position != 1 ||
		len(d.Photos[0].Annotations) != 2 {
		t.Fatalf("detail setelah hapus foto: %+v", d)
	}
	// tab edit yang masih memegang versi 1 mendapat konflik
	if _, err := r.edit(out, 1, ep(p[0], "x"), ep(p[2])); errCode(err) != "VERSION_CONFLICT" {
		t.Fatalf("edit basi: %v", err)
	}
	// foto yang bukan attached → 404
	if _, err := r.del.DeletePhoto(bg, p[1].ID); errCode(err) != "NOT_FOUND" {
		t.Fatalf("hapus foto yang sudah dihapus: %v", err)
	}
	if _, err := r.del.DeletePhoto(bg, r.uploaded(t, nil).ID); errCode(err) != "NOT_FOUND" {
		t.Fatalf("hapus foto pending: %v", err)
	}
	// hapus foto terakhir → submission ikut hilang
	if _, err := r.del.DeletePhoto(bg, p[0].ID); err != nil {
		t.Fatal(err)
	}
	res, err = r.del.DeletePhoto(bg, p[2].ID)
	if err != nil || !res.SubmissionDeleted || res.Status != usecase.DeleteStatusDeleted {
		t.Fatalf("hapus foto terakhir: %+v %v", res, err)
	}
	if n := r.rows(t, `SELECT count(*) FROM submissions WHERE id = $1`, out.SubmissionID); n != 0 {
		t.Fatal("submission tanpa foto harus ikut terhapus")
	}
}

// AC 14: SSD error → 202 cleanup_pending, data tidak terlihat, cleanup menyelesaikan sisanya.
func TestAdminDeleteFileFailureLeavesCleanupPending(t *testing.T) {
	r := newDeleteRig(t)
	out, p := r.submitted(t, nil, 2)
	r.fails.fail[p[1].StorageKey] = true
	res, err := r.del.DeleteSubmission(bg, out.SubmissionID)
	if err != nil || res.Status != usecase.DeleteStatusCleanupPending || res.FilesDeleted != 1 || res.FileFailures == 0 {
		t.Fatalf("hasil = %+v %v", res, err)
	}
	if r.fileExists(p[0]) || !r.fileExists(p[1]) {
		t.Fatal("file yang berhasil dihapus hilang; yang gagal tetap ada")
	}
	// baris foto yang filenya gagal tetap ada (deleted) sehingga tidak pernah ada baris tanpa file
	if n := r.rows(t, `SELECT count(*) FROM photos WHERE id = $1 AND status = 'deleted'`, p[1].ID); n != 1 {
		t.Fatal("baris foto yang filenya gagal dihapus harus dipertahankan")
	}
	if n := r.rows(t, `SELECT count(*) FROM photos WHERE id = $1`, p[0].ID); n != 0 {
		t.Fatal("baris foto yang filenya terhapus harus ikut terhapus")
	}
	if _, err := r.rd.Submission(bg, usecase.Caller{EditToken: out.EditToken}, out.SubmissionID); errCode(err) != "NOT_FOUND" {
		t.Fatalf("data harus tersembunyi: %v", err)
	}
	if _, err := r.del.DeleteSubmission(bg, out.SubmissionID); errCode(err) != "NOT_FOUND" {
		t.Fatalf("hapus ulang saat cleanup_pending: %v", err)
	}
	// cleanup berkala (SSD sudah pulih) menyelesaikan file, baris foto, dan baris submission
	cu := usecase.NewCleanup(postgres.NewCleanupRepo(r.pool), r.store, r.clock, usecase.CleanupConfig{
		RetentionDays: 90, PendingTTL: 24 * time.Hour, Batch: 10, TmpMaxAge: time.Hour, OrphanMinAge: 24 * time.Hour})
	rep, err := cu.RunOnce(bg)
	if err != nil || rep.FilesDeleted != 1 || rep.SubmissionsDeleted != 1 {
		t.Fatalf("cleanup: %+v %v", rep, err)
	}
	if r.fileExists(p[1]) || r.rows(t, `SELECT count(*) FROM submissions WHERE id = $1`, out.SubmissionID) != 0 {
		t.Fatal("cleanup harus menuntaskan sisa hapus")
	}
}

// AC 15: edit pengguna yang berbarengan dengan hapus admin berakhir 404/409 dan tidak pernah
// menghidupkan kembali data yang dihapus.
func TestAdminDeleteRacesWithEdit(t *testing.T) {
	r := newDeleteRig(t)
	for i := 0; i < 5; i++ {
		out, p := r.submitted(t, nil, 2)
		fresh := r.uploaded(t, nil)
		var wg sync.WaitGroup
		var editErr, delErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, editErr = r.edit(out, 1, ep(p[0], "edit"), ep(fresh, "baru"))
		}()
		go func() {
			defer wg.Done()
			_, delErr = r.del.DeleteSubmission(bg, out.SubmissionID)
		}()
		wg.Wait()
		if delErr != nil {
			t.Fatalf("hapus: %v", delErr)
		}
		if editErr != nil && errCode(editErr) != "NOT_FOUND" && errCode(editErr) != "VERSION_CONFLICT" {
			t.Fatalf("edit berbarengan: %v", editErr)
		}
		if n := r.rows(t, `SELECT count(*) FROM photos WHERE submission_id = $1 AND status = 'attached'`, out.SubmissionID); n != 0 {
			t.Fatalf("putaran %d: ada foto attached setelah hapus (edit=%v)", i, editErr)
		}
		if n := r.rows(t, `SELECT count(*) FROM submissions WHERE id = $1 AND deleted_at IS NULL`, out.SubmissionID); n != 0 {
			t.Fatalf("putaran %d: submission hidup kembali", i)
		}
	}
}

func TestAdminDeleteMany(t *testing.T) {
	r := newDeleteRig(t)
	a, _ := r.submitted(t, nil, 1)
	b, _ := r.submitted(t, nil, 2)
	missing := uuid.New()
	res, err := r.del.DeleteMany(bg, []uuid.UUID{a.SubmissionID, missing, b.SubmissionID})
	if err != nil || len(res) != 3 || res[0].Status != usecase.DeleteStatusDeleted || res[1].Status != usecase.DeleteStatusNotFound ||
		res[1].SubmissionID != missing || res[2].PhotosDeleted != 2 {
		t.Fatalf("hapus massal: %+v %v", res, err)
	}
	many := make([]uuid.UUID, 51)
	for i := range many {
		many[i] = uuid.New()
	}
	for name, ids := range map[string][]uuid.UUID{"kosong": nil, "51": many, "duplikat": {a.SubmissionID, a.SubmissionID}, "nol": {uuid.Nil}} {
		if _, err := r.del.DeleteMany(bg, ids); errCode(err) != "INVALID_IDS" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
