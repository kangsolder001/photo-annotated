package postgres_test

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
)

type cleanRig struct {
	*rig
	clean *postgres.CleanupRepo
}

func newCleanRig(t *testing.T) *cleanRig {
	r := newRig(t)
	return &cleanRig{rig: r, clean: postgres.NewCleanupRepo(r.pool)}
}

func (r *cleanRig) setCreated(t *testing.T, table string, id uuid.UUID, at time.Time) {
	t.Helper()
	if _, err := r.pool.Exec(ctx, `UPDATE `+table+` SET created_at=$2 WHERE id=$1`, id, at); err != nil {
		t.Fatal(err)
	}
}

func (r *cleanRig) status(t *testing.T, id uuid.UUID) domain.PhotoStatus {
	t.Helper()
	p, err := r.photos.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Status
}

func (r *cleanRig) pendingPhoto(t *testing.T, createdAt time.Time) *domain.Photo {
	t.Helper()
	p := validPhoto()
	p.CreatedAt = createdAt
	if err := r.photos.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClaimStalePending(t *testing.T) {
	r := newCleanRig(t)
	cutoff := now().Add(-24 * time.Hour)
	stale1 := r.pendingPhoto(t, cutoff.Add(-time.Microsecond))
	stale2 := r.pendingPhoto(t, cutoff.Add(-time.Hour))
	atCutoff := r.pendingPhoto(t, cutoff) // tepat pada batas: belum basi
	young := r.pendingPhoto(t, now())
	rejected := r.pendingPhoto(t, cutoff.Add(-time.Hour)) // status lain tidak disentuh
	_, _ = r.pool.Exec(ctx, `UPDATE photos SET status='rejected' WHERE id=$1`, rejected.ID)
	sub := newSub(nil)
	_ = r.subs.Create(ctx, sub)
	attached := r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, attached.ID, sub.ID, 0, nil)
	r.setCreated(t, "photos", attached.ID, cutoff.Add(-48*time.Hour))

	n, err := r.clean.ClaimStalePending(ctx, cutoff, 100)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v, want 2", n, err)
	}
	for id, want := range map[uuid.UUID]domain.PhotoStatus{
		stale1.ID: domain.PhotoDeleted, stale2.ID: domain.PhotoDeleted,
		atCutoff.ID: domain.PhotoPending, young.ID: domain.PhotoPending,
		rejected.ID: domain.PhotoRejected, attached.ID: domain.PhotoAttached,
	} {
		if got := r.status(t, id); got != want {
			t.Errorf("status %s = %s, want %s", id, got, want)
		}
	}
	// idempotent
	if n, _ := r.clean.ClaimStalePending(ctx, cutoff, 100); n != 0 {
		t.Fatalf("putaran kedua n=%d", n)
	}
}

func TestClaimStalePendingRespectsLimit(t *testing.T) {
	r := newCleanRig(t)
	cutoff := now()
	for i := 0; i < 5; i++ {
		r.pendingPhoto(t, cutoff.Add(-time.Hour))
	}
	n, err := r.clean.ClaimStalePending(ctx, cutoff, 3)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, _ := r.clean.ClaimStalePending(ctx, cutoff, 3); n != 2 {
		t.Fatalf("sisa = %d, want 2", n)
	}
}

// Setelah di-claim, foto tidak bisa lagi dilampirkan; inilah yang melindungi klien yang
// melampirkan foto tepat pada saat cleanup berjalan.
func TestClaimedPhotoCannotBeAttached(t *testing.T) {
	r := newCleanRig(t)
	p := r.uploadedPhoto(t, nil)
	r.setCreated(t, "photos", p.ID, now().Add(-48*time.Hour))
	if n, _ := r.clean.ClaimStalePending(ctx, now().Add(-24*time.Hour), 10); n != 1 {
		t.Fatalf("n = %d", n)
	}
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	if err := r.photos.Attach(ctx, p.ID, s.ID, 0, nil); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("Attach pada foto yang di-claim harus konflik: %v", err)
	}
}

func TestClaimVersusAttachRaceNeverLeavesBoth(t *testing.T) {
	r := newCleanRig(t)
	const n = 20
	photos := make([]*domain.Photo, n)
	subs := make([]*domain.Submission, n)
	for i := range photos {
		photos[i] = r.uploadedPhoto(t, nil)
		r.setCreated(t, "photos", photos[i].ID, now().Add(-48*time.Hour))
		subs[i] = newSub(nil)
		if err := r.subs.Create(ctx, subs[i]); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_, _ = r.clean.ClaimStalePending(ctx, now().Add(-24*time.Hour), 4)
		}
	}()
	go func() {
		defer wg.Done()
		for i := range photos {
			_ = r.photos.Attach(ctx, photos[i].ID, subs[i].ID, 0, nil)
		}
	}()
	wg.Wait()
	_, _ = r.clean.ClaimStalePending(ctx, now().Add(-24*time.Hour), 100)
	for i, p := range photos {
		got, _ := r.photos.Get(ctx, p.ID)
		switch got.Status {
		case domain.PhotoAttached:
			if got.SubmissionID == nil || *got.SubmissionID != subs[i].ID {
				t.Fatalf("foto attached tanpa submission yang benar: %+v", got)
			}
		case domain.PhotoDeleted:
			if got.SubmissionID != nil {
				t.Fatalf("foto yang di-claim tidak boleh punya submission: %+v", got)
			}
		default:
			t.Fatalf("status akhir tak terduga: %s", got.Status)
		}
	}
}

func TestClaimSkipsLockedRows(t *testing.T) {
	r := newCleanRig(t)
	locked := r.pendingPhoto(t, now().Add(-48*time.Hour))
	free := r.pendingPhoto(t, now().Add(-48*time.Hour))

	// kunci baris `locked` di transaksi terpisah
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM photos WHERE id=$1 FOR UPDATE`, locked.ID); err != nil {
		t.Fatal(err)
	}
	n, err := r.clean.ClaimStalePending(ctx, now().Add(-24*time.Hour), 10) // tidak boleh menunggu kunci
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1 (baris terkunci dilewati)", n, err)
	}
	if r.status(t, free.ID) != domain.PhotoDeleted {
		t.Fatal("baris yang bebas harus di-claim")
	}
	_ = tx.Rollback(ctx)
	if r.status(t, locked.ID) != domain.PhotoPending {
		t.Fatal("baris yang terkunci tidak boleh tersentuh")
	}
}

func TestListDeletablePhotosPaginates(t *testing.T) {
	r := newCleanRig(t)
	var want []uuid.UUID
	for i := 0; i < 5; i++ {
		p := r.pendingPhoto(t, now())
		st := "rejected"
		if i%2 == 0 {
			st = "deleted"
		}
		_, _ = r.pool.Exec(ctx, `UPDATE photos SET status=$2 WHERE id=$1`, p.ID, st)
		want = append(want, p.ID)
	}
	r.pendingPhoto(t, now())    // pending: tidak ikut
	_ = r.uploadedPhoto(t, nil) // pending: tidak ikut
	sort.Slice(want, func(i, j int) bool { return want[i].String() < want[j].String() })

	var got []uuid.UUID
	after := uuid.Nil
	for {
		refs, err := r.clean.ListDeletablePhotos(ctx, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) == 0 {
			break
		}
		for _, ref := range refs {
			if !domain.IsValidStorageKey(ref.StorageKey) {
				t.Fatalf("storage key: %q", ref.StorageKey)
			}
			got = append(got, ref.ID)
		}
		after = refs[len(refs)-1].ID
	}
	if len(got) != 5 {
		t.Fatalf("got %d, want 5", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("urutan/isi salah di %d: %v vs %v", i, got, want)
		}
	}
}

func TestDeletePhotoRowOnlyRejectedOrDeleted(t *testing.T) {
	r := newCleanRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	attached := r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, attached.ID, s.ID, 0, nil)
	_ = r.subs.AddAnnotations(ctx, attached.ID, []domain.Annotation{{Text: "x", X: 0, Y: 0, W: 0.5, H: 0.5}})
	pending := r.pendingPhoto(t, now())
	gone := r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, gone.ID, s.ID, 1, nil)
	_ = r.subs.AddAnnotations(ctx, gone.ID, []domain.Annotation{{Text: "y", X: 0, Y: 0, W: 0.5, H: 0.5}})
	_, _ = r.pool.Exec(ctx, `UPDATE photos SET status='deleted' WHERE id=$1`, gone.ID)

	for _, id := range []uuid.UUID{attached.ID, pending.ID, gone.ID} {
		if err := r.clean.DeletePhotoRow(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.photos.Get(ctx, attached.ID); err != nil {
		t.Fatal("foto attached tidak boleh terhapus")
	}
	if _, err := r.photos.Get(ctx, pending.ID); err != nil {
		t.Fatal("foto pending tidak boleh terhapus")
	}
	if _, err := r.photos.Get(ctx, gone.ID); err == nil {
		t.Fatal("foto deleted harus terhapus")
	}
	var n int
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM annotations WHERE photo_id=$1`, gone.ID).Scan(&n)
	if n != 0 {
		t.Fatal("anotasi foto yang dihapus harus ikut terhapus (CASCADE)")
	}
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM annotations WHERE photo_id=$1`, attached.ID).Scan(&n)
	if n != 1 {
		t.Fatal("anotasi foto yang tidak dihapus harus utuh")
	}
	// idempotent
	if err := r.clean.DeletePhotoRow(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
}

func (r *cleanRig) submissionWithPhotos(t *testing.T, created time.Time, nPhotos int) (*domain.Submission, []*domain.Photo) {
	t.Helper()
	s := newSub(nil)
	if err := r.subs.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	var ps []*domain.Photo
	for i := 0; i < nPhotos; i++ {
		p := r.uploadedPhoto(t, nil)
		if err := r.photos.Attach(ctx, p.ID, s.ID, i, nil); err != nil {
			t.Fatal(err)
		}
		_ = r.subs.AddAnnotations(ctx, p.ID, []domain.Annotation{{Text: "a", X: 0, Y: 0, W: 0.5, H: 0.5}})
		ps = append(ps, p)
	}
	r.setCreated(t, "submissions", s.ID, created)
	return s, ps
}

func TestExpireSubmissionsAndDeleteEmpty(t *testing.T) {
	r := newCleanRig(t)
	cutoff := now().Add(-90 * 24 * time.Hour)
	old, oldPhotos := r.submissionWithPhotos(t, cutoff.Add(-time.Microsecond), 2)
	atCutoff, atPhotos := r.submissionWithPhotos(t, cutoff, 1) // tepat pada batas: belum kedaluwarsa
	fresh, freshPhotos := r.submissionWithPhotos(t, now(), 1)

	// belum ada yang bisa dihapus: submission lama masih punya baris foto
	if n, err := r.clean.DeleteEmptyExpiredSubmissions(ctx, cutoff, 100); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v (submission masih punya foto)", n, err)
	}
	n, err := r.clean.ExpireSubmissions(ctx, cutoff, 100)
	if err != nil || n != 2 {
		t.Fatalf("foto yang ditandai = %d err=%v, want 2", n, err)
	}
	for _, p := range oldPhotos {
		if r.status(t, p.ID) != domain.PhotoDeleted {
			t.Fatal("foto submission kedaluwarsa harus deleted")
		}
	}
	if r.status(t, atPhotos[0].ID) != domain.PhotoAttached || r.status(t, freshPhotos[0].ID) != domain.PhotoAttached {
		t.Fatal("submission yang belum kedaluwarsa tidak boleh tersentuh")
	}
	// tanpa menghapus baris foto, submission belum boleh dihapus ("file dulu, baris kemudian")
	if n, _ := r.clean.DeleteEmptyExpiredSubmissions(ctx, cutoff, 100); n != 0 {
		t.Fatalf("submission dengan baris foto tidak boleh dihapus: n=%d", n)
	}
	// ExpireSubmissions kedua melewati submission yang semua fotonya sudah deleted
	if n, _ := r.clean.ExpireSubmissions(ctx, cutoff, 100); n != 0 {
		t.Fatalf("putaran kedua n=%d", n)
	}
	for _, p := range oldPhotos {
		_ = r.clean.DeletePhotoRow(ctx, p.ID)
	}
	n, err = r.clean.DeleteEmptyExpiredSubmissions(ctx, cutoff, 100)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1", n, err)
	}
	if _, err := r.subs.Get(ctx, old.ID); err == nil {
		t.Fatal("submission lama harus terhapus")
	}
	for _, id := range []uuid.UUID{atCutoff.ID, fresh.ID} {
		if _, err := r.subs.Get(ctx, id); err != nil {
			t.Fatalf("submission %s tidak boleh terhapus: %v", id, err)
		}
	}
}

func TestExpireSubmissionsLimitCountsSubmissionsAndDoesNotStarve(t *testing.T) {
	r := newCleanRig(t)
	cutoff := now()
	var subs []*domain.Submission
	for i := 0; i < 4; i++ {
		s, _ := r.submissionWithPhotos(t, cutoff.Add(-time.Duration(10-i)*time.Hour), 1)
		subs = append(subs, s)
	}
	// limit 2 submission per panggilan; submission yang sudah ditandai dilewati sehingga
	// panggilan berikutnya maju ke yang berikutnya (tidak terjebak di kepala antrean)
	total := 0
	for i := 0; i < 3; i++ {
		n, err := r.clean.ExpireSubmissions(ctx, cutoff, 2)
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	if total != 4 {
		t.Fatalf("total foto ditandai = %d, want 4", total)
	}
	_ = subs
}

func TestDeleteEmptyExpiredSubmissionsDeletesSubmissionWithoutPhotos(t *testing.T) {
	r := newCleanRig(t)
	cutoff := now()
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	r.setCreated(t, "submissions", s.ID, cutoff.Add(-time.Hour))
	if n, err := r.clean.DeleteEmptyExpiredSubmissions(ctx, cutoff, 10); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestExistingStorageKeys(t *testing.T) {
	r := newCleanRig(t)
	a, b := r.pendingPhoto(t, now()), r.pendingPhoto(t, now())
	_, _ = r.pool.Exec(ctx, `UPDATE photos SET status='deleted' WHERE id=$1`, b.ID)
	got, err := r.clean.ExistingStorageKeys(ctx, []string{a.StorageKey, b.StorageKey, "2026/01/01/00000000-0000-0000-0000-000000000000.webp"})
	if err != nil {
		t.Fatal(err)
	}
	if !got[a.StorageKey] || !got[b.StorageKey] || len(got) != 2 {
		t.Fatalf("got %v (status apa pun dihitung ada; kunci asing tidak)", got)
	}
	if got, err := r.clean.ExistingStorageKeys(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("input kosong: %v %v", got, err)
	}
}
