package usecase_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type editRig struct {
	*readRig
	ed *usecase.Edit
}

func newEditRig(t *testing.T, noEdit bool) *editRig {
	t.Helper()
	base := newReadRig(t, noEdit)
	var tok usecase.EditTokenSigner = base.edit
	if noEdit {
		tok = nil
	}
	return &editRig{readRig: base, ed: usecase.NewEdit(base.photos, base.subs, postgres.NewTxManager(base.pool), base.store, tok, base.clock,
		usecase.EditConfig{SubmitConfig: usecase.SubmitConfig{MaxPhotos: 5, MaxAnnotationsPerPhoto: 50, MaxAnnotationTextLen: 1000}, Window: 24 * time.Hour})}
}

// ep membuat entri foto untuk request edit, dengan satu anotasi per teks.
func ep(p *domain.Photo, texts ...string) usecase.SubmitPhoto {
	sp := usecase.SubmitPhoto{PhotoID: p.ID, Annotations: []domain.Annotation{}}
	for i, tx := range texts {
		sp.Annotations = append(sp.Annotations, domain.Annotation{Text: tx, X: 0.01 * float64(i%50), Y: 0.1, W: 0.2, H: 0.2})
	}
	return sp
}

func (r *editRig) detail(t *testing.T, id uuid.UUID) *domain.SubmissionDetail {
	t.Helper()
	d, err := r.subs.GetDetail(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (r *editRig) edit(out *usecase.SubmitOutput, version int, photos ...usecase.SubmitPhoto) (*usecase.EditOutput, error) {
	return r.ed.Edit(bg, usecase.EditInput{
		SubmissionID: out.SubmissionID, Caller: usecase.Caller{EditToken: out.EditToken}, Version: version, Photos: photos,
	})
}

func ids(d *domain.SubmissionDetail) []uuid.UUID {
	var out []uuid.UUID
	for _, p := range d.Photos {
		out = append(out, p.Photo.ID)
	}
	return out
}

func sameIDs(got []uuid.UUID, want ...*domain.Photo) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i].ID {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- perubahan isi

func TestEditReplacesAnnotationsOnly(t *testing.T) {
	r := newEditRig(t, false)
	out, photos := r.submitted(t, nil, 2)
	r.clock.t = r.clock.t.Add(time.Hour)

	res, err := r.edit(out, 1, ep(photos[0], "diperbarui", "tambahan"), ep(photos[1]))
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != 2 || res.SubmissionID != out.SubmissionID || !res.UpdatedAt.Equal(r.clock.t) {
		t.Fatalf("hasil: %+v", res)
	}
	d := r.detail(t, out.SubmissionID)
	if d.Submission.Version != 2 || !d.Submission.UpdatedAt.Equal(r.clock.t) || !d.Submission.CreatedAt.Equal(out.CreatedAt) {
		t.Fatalf("submission: %+v", d.Submission)
	}
	if got := d.Photos[0].Annotations; len(got) != 2 || got[0].Text != "diperbarui" || got[1].Text != "tambahan" {
		t.Fatalf("anotasi foto 1 harus diganti seluruhnya: %+v", got)
	}
	if len(d.Photos[1].Annotations) != 0 {
		t.Fatalf("foto 2 dikirim tanpa anotasi: %+v", d.Photos[1].Annotations)
	}
	if !sameIDs(ids(d), photos...) {
		t.Fatal("foto tidak boleh berubah")
	}
}

func TestEditRemoveAddAndReorderTogether(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 3) // p[0], p[1], p[2]
	fresh := r.uploaded(t, nil)

	// buang p[1], tambah fresh, urutan baru: p[2], fresh, p[0]
	if _, err := r.edit(out, 1, ep(p[2], "c"), ep(fresh, "baru"), ep(p[0], "a")); err != nil {
		t.Fatal(err)
	}
	d := r.detail(t, out.SubmissionID)
	if !sameIDs(ids(d), p[2], fresh, p[0]) {
		t.Fatalf("urutan: %v", ids(d))
	}
	for i, ph := range d.Photos {
		if ph.Photo.Position != i || ph.Photo.Status != domain.PhotoAttached {
			t.Fatalf("foto[%d]: %+v", i, ph.Photo)
		}
	}
	if d.Photos[1].Annotations[0].Text != "baru" {
		t.Fatalf("anotasi foto baru: %+v", d.Photos[1].Annotations)
	}
	// foto yang dibuang: deleted, anotasinya hilang, file TETAP ada sampai cleanup
	if r.photoStatus(t, p[1]) != domain.PhotoDeleted {
		t.Fatalf("status foto dibuang = %s", r.photoStatus(t, p[1]))
	}
	var n int
	if err := r.pool.QueryRow(bg, `SELECT count(*) FROM annotations WHERE photo_id=$1`, p[1].ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("anotasi foto dibuang harus terhapus: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, p[1].StorageKey)); err != nil {
		t.Fatalf("file foto yang dibuang dihapus oleh cleanup, bukan saat edit: %v", err)
	}
}

func TestEditCanReduceToOnePhotoButNotZero(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 2)
	_, err := r.edit(out, 1)
	wantCode(t, err, "INVALID_PHOTO_COUNT")
	if r.detail(t, out.SubmissionID).Submission.Version != 1 {
		t.Fatal("edit yang ditolak tidak boleh menaikkan versi")
	}
	if _, err := r.edit(out, 1, ep(p[1], "satu-satunya")); err != nil {
		t.Fatal(err)
	}
	if d := r.detail(t, out.SubmissionID); len(d.Photos) != 1 || d.Photos[0].Photo.ID != p[1].ID || d.Photos[0].Photo.Position != 0 {
		t.Fatalf("detail: %+v", d.Photos)
	}
}

func TestEditAtLimitsAndNormalization(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 1)
	var texts []string
	for i := 0; i < 50; i++ {
		texts = append(texts, "n")
	}
	if _, err := r.edit(out, 1, ep(p[0], texts...)); err != nil {
		t.Fatalf("50 anotasi harus diterima: %v", err)
	}
	if _, err := r.edit(out, 2, ep(p[0], "  rapi \r\n dong  ")); err != nil {
		t.Fatal(err)
	}
	if got := r.detail(t, out.SubmissionID).Photos[0].Annotations[0].Text; got != "rapi \n dong" {
		t.Fatalf("teks harus dinormalisasi: %q", got)
	}
}

// ---------------------------------------------------------------- versi & konkurensi

func TestEditVersionConflictChangesNothing(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 2)
	if _, err := r.edit(out, 1, ep(p[0], "v2"), ep(p[1])); err != nil {
		t.Fatal(err)
	}
	fresh := r.uploaded(t, nil)
	// klien lain masih memegang versi 1
	_, err := r.edit(out, 1, ep(p[0], "basi"), ep(fresh, "foto baru"))
	de, ok := domain.AsError(err)
	if !ok || de.Code != "VERSION_CONFLICT" || de.Kind != domain.KindConflict {
		t.Fatalf("err = %v", err)
	}
	d := r.detail(t, out.SubmissionID)
	if d.Submission.Version != 2 || d.Photos[0].Annotations[0].Text != "v2" || !sameIDs(ids(d), p...) {
		t.Fatalf("edit basi tidak boleh mengubah apa pun: %+v", d)
	}
	if r.photoStatus(t, fresh) != domain.PhotoPending {
		t.Fatal("foto baru dari edit yang gagal harus tetap pending")
	}
	if _, err := r.edit(out, 2, ep(p[0], "v3"), ep(p[1])); err != nil {
		t.Fatalf("dengan versi terkini harus berhasil: %v", err)
	}
}

func TestEditRejectsInvalidVersion(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 1)
	for _, v := range []int{0, -1} {
		_, err := r.edit(out, v, ep(p[0], "x"))
		wantCode(t, err, "INVALID_VERSION")
	}
}

func TestEditConcurrentSameVersionExactlyOneWins(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 1)
	const n = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.edit(out, 1, ep(p[0], "dari klien "+string(rune('A'+i))))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case codeOf(err) == "VERSION_CONFLICT":
				conflicts++
			default:
				t.Errorf("error tak terduga: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || conflicts != n-1 {
		t.Fatalf("menang=%d konflik=%d", wins, conflicts)
	}
	d := r.detail(t, out.SubmissionID)
	if d.Submission.Version != 2 || len(d.Photos[0].Annotations) != 1 || !strings.HasPrefix(d.Photos[0].Annotations[0].Text, "dari klien") {
		t.Fatalf("hanya satu perubahan utuh yang boleh tersimpan: %+v", d.Photos[0].Annotations)
	}
}

// ---------------------------------------------------------------- jendela edit

func TestEditWindow(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 1)
	created := out.CreatedAt

	// satu nanodetik sebelum batas: masih boleh
	r.clock.t = created.Add(24*time.Hour - time.Microsecond)
	if _, err := r.edit(out, 1, ep(p[0], "tepat sebelum batas")); err != nil {
		t.Fatalf("sebelum batas harus boleh: %v", err)
	}
	// edit TIDAK memperpanjang jendela: batas tetap created_at + 24 jam
	r.clock.t = created.Add(24 * time.Hour)
	_, err := r.edit(out, 2, ep(p[0], "terlambat"))
	de, ok := domain.AsError(err)
	if !ok || de.Code != "EDIT_WINDOW_CLOSED" || de.Kind != domain.KindForbidden {
		t.Fatalf("tepat pada batas harus EDIT_WINDOW_CLOSED: %v", err)
	}
	r.clock.t = created.Add(48 * time.Hour)
	if _, err := r.edit(out, 2, ep(p[0], "jauh terlambat")); codeOf(err) != "EDIT_WINDOW_CLOSED" {
		t.Fatalf("err = %v", err)
	}
	if d := r.detail(t, out.SubmissionID); d.Photos[0].Annotations[0].Text != "tepat sebelum batas" || d.Submission.Version != 2 {
		t.Fatalf("edit yang ditolak jendela tidak boleh mengubah data: %+v", d.Photos[0].Annotations)
	}
}

func TestEditWindowIsCheckedBeforeVersionButAfterAuthorization(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 1)
	r.clock.t = out.CreatedAt.Add(30 * time.Hour)
	// versi basi DAN jendela tertutup → jendela yang dilaporkan
	if _, err := r.edit(out, 99, ep(p[0], "x")); codeOf(err) != "EDIT_WINDOW_CLOSED" {
		t.Fatalf("err = %v", err)
	}
	// tanpa hak: tetap 404 seragam, bukan membocorkan jendela tertutup
	_, err := r.ed.Edit(bg, usecase.EditInput{SubmissionID: out.SubmissionID, Caller: usecase.Caller{EditToken: "salah"}, Version: 1, Photos: []usecase.SubmitPhoto{ep(p[0], "x")}})
	isNotFound(t, err)
}

// ---------------------------------------------------------------- otorisasi

func TestEditAuthorization(t *testing.T) {
	r := newEditRig(t, false)
	mine, mp := r.submitted(t, nil, 1)
	other, _ := r.submitted(t, nil, 1)
	alice := uuid.New()
	do := func(c usecase.Caller, id uuid.UUID) error {
		_, err := r.ed.Edit(bg, usecase.EditInput{SubmissionID: id, Caller: c, Version: 1, Photos: []usecase.SubmitPhoto{ep(mp[0], "x")}})
		return err
	}
	for name, err := range map[string]error{
		"tanpa token":            do(usecase.Caller{}, mine.SubmissionID),
		"token salah":            do(usecase.Caller{EditToken: "salah"}, mine.SubmissionID),
		"token orang lain":       do(usecase.Caller{EditToken: other.EditToken}, mine.SubmissionID),
		"id tidak ada":           do(usecase.Caller{EditToken: mine.EditToken}, uuid.New()),
		"user tanpa kepemilikan": do(usecase.Caller{UserID: &alice}, mine.SubmissionID),
	} {
		if err == nil {
			t.Errorf("%s: seharusnya ditolak", name)
			continue
		}
		isNotFound(t, err)
	}
	// admin boleh membaca tetapi tidak boleh mengedit: 403 yang jelas (ia memang tahu submission-nya ada)
	err := do(usecase.Caller{Admin: true}, mine.SubmissionID)
	de, ok := domain.AsError(err)
	if !ok || de.Code != "EDIT_NOT_ALLOWED" || de.Kind != domain.KindForbidden {
		t.Fatalf("admin: %v", err)
	}
	if r.detail(t, mine.SubmissionID).Submission.Version != 1 {
		t.Fatal("penolakan tidak boleh mengubah versi")
	}
}

func TestEditByOwnerUser(t *testing.T) {
	r := newEditRig(t, true) // mode otp: tanpa token edit
	alice, bob := uuid.New(), uuid.New()
	out, p := r.submitted(t, &alice, 1)
	run := func(owner *uuid.UUID) error {
		_, err := r.ed.Edit(bg, usecase.EditInput{SubmissionID: out.SubmissionID, Caller: usecase.Caller{UserID: owner}, Version: 1, Photos: []usecase.SubmitPhoto{ep(p[0], "oleh pemilik")}})
		return err
	}
	isNotFound(t, run(&bob))
	if err := run(&alice); err != nil {
		t.Fatalf("pemilik harus boleh: %v", err)
	}
	// foto baru harus milik pemilik yang sama
	foreign := r.uploaded(t, &bob)
	_, err := r.ed.Edit(bg, usecase.EditInput{SubmissionID: out.SubmissionID, Caller: usecase.Caller{UserID: &alice}, Version: 2,
		Photos: []usecase.SubmitPhoto{ep(p[0], "x"), ep(foreign, "curian")}})
	wantCode(t, err, "PHOTO_NOT_FOUND")
}

// ---------------------------------------------------------------- validasi

func TestEditShapeValidation(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 2)
	six := make([]usecase.SubmitPhoto, 6)
	for i := range six {
		six[i] = ep(p[0])
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = "x"
	}
	cases := map[string]struct {
		photos []usecase.SubmitPhoto
		code   string
	}{
		"melebihi batas foto": {six, "INVALID_PHOTO_COUNT"},
		"foto kembar":         {[]usecase.SubmitPhoto{ep(p[0]), ep(p[0])}, "DUPLICATE_PHOTO"},
		"51 anotasi":          {[]usecase.SubmitPhoto{ep(p[0], many...)}, "TOO_MANY_ANNOTATIONS"},
		"teks kosong":         {[]usecase.SubmitPhoto{ep(p[0], "   ")}, "INVALID_ANNOTATION_TEXT"},
		"bbox salah":          {[]usecase.SubmitPhoto{{PhotoID: p[0].ID, Annotations: []domain.Annotation{{Text: "x", X: 0.9, Y: 0, W: 0.5, H: 0.5}}}}, "INVALID_BBOX"},
	}
	for name, c := range cases {
		_, err := r.edit(out, 1, c.photos...)
		de, ok := domain.AsError(err)
		if !ok || de.Code != c.code || de.Kind != domain.KindInvalid {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}
	}
	if r.detail(t, out.SubmissionID).Submission.Version != 1 {
		t.Fatal("validasi gagal tidak boleh menaikkan versi")
	}
}

// ---------------------------------------------------------------- foto baru

func TestEditNewPhotoProblems(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 1)

	notUploaded := r.pendingOnly(t)
	_, err := r.edit(out, 1, ep(p[0]), ep(notUploaded))
	wantCode(t, err, "PHOTO_NOT_UPLOADED")

	_, err = r.edit(out, 1, ep(p[0]), ep(&domain.Photo{ID: uuid.New()}))
	wantCode(t, err, "PHOTO_NOT_FOUND")

	// sudah dipakai submission lain
	_, otherPhotos := r.submitted(t, nil, 1)
	_, err = r.edit(out, 1, ep(p[0]), ep(otherPhotos[0]))
	wantCode(t, err, "PHOTO_NOT_AVAILABLE")

	// file hilang → hanya foto itu yang rejected
	gone := r.uploaded(t, nil)
	if err := os.Remove(filepath.Join(r.dir, gone.StorageKey)); err != nil {
		t.Fatal(err)
	}
	_, err = r.edit(out, 1, ep(p[0]), ep(gone))
	wantCode(t, err, "PHOTO_FILE_INVALID")
	if r.photoStatus(t, gone) != domain.PhotoRejected {
		t.Fatal("foto bermasalah harus rejected")
	}
	if r.detail(t, out.SubmissionID).Submission.Version != 1 {
		t.Fatal("tidak ada edit yang boleh tersimpan")
	}
}

func TestEditCannotReAddARemovedPhoto(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 2)
	if _, err := r.edit(out, 1, ep(p[0], "a")); err != nil { // p[1] dibuang
		t.Fatal(err)
	}
	_, err := r.edit(out, 2, ep(p[0], "a"), ep(p[1], "kembali"))
	de, ok := domain.AsError(err)
	if !ok || de.Code != "PHOTO_NOT_AVAILABLE" || de.Kind != domain.KindConflict {
		t.Fatalf("foto yang sudah dibuang tidak boleh dihidupkan lagi: %v", err)
	}
}

// ---------------------------------------------------------------- atomisitas

func TestEditIsAtomicWhenALaterStepFails(t *testing.T) {
	r := newEditRig(t, false)
	out, p := r.submitted(t, nil, 2)
	f1, f2 := r.uploaded(t, nil), r.uploaded(t, nil)

	flip := &flipOnSecondAttach{PhotoRepo: r.photos, pool: r.pool}
	ed := usecase.NewEdit(flip, r.subs, postgres.NewTxManager(r.pool), r.store, r.subRig.edit, r.clock,
		usecase.EditConfig{SubmitConfig: usecase.SubmitConfig{MaxPhotos: 5, MaxAnnotationsPerPhoto: 50, MaxAnnotationTextLen: 1000}, Window: 24 * time.Hour})

	// edit besar: buang p[1], ubah p[0], tambah f1 dan f2 (Attach kedua gagal di tengah transaksi)
	_, err := ed.Edit(bg, usecase.EditInput{SubmissionID: out.SubmissionID, Caller: usecase.Caller{EditToken: out.EditToken}, Version: 1,
		Photos: []usecase.SubmitPhoto{ep(p[0], "TIDAK BOLEH TERSIMPAN"), ep(f1, "f1"), ep(f2, "f2")}})
	wantCode(t, err, "PHOTO_NOT_AVAILABLE")

	d := r.detail(t, out.SubmissionID)
	if d.Submission.Version != 1 || !sameIDs(ids(d), p...) {
		t.Fatalf("rollback harus mengembalikan semuanya: versi %d foto %v", d.Submission.Version, ids(d))
	}
	if got := d.Photos[0].Annotations; len(got) != 2 || got[0].Text != "pertama" {
		t.Fatalf("anotasi lama harus utuh: %+v", got)
	}
	if r.photoStatus(t, p[1]) != domain.PhotoAttached {
		t.Fatal("foto yang akan dibuang harus tetap attached setelah rollback")
	}
	if r.photoStatus(t, f1) != domain.PhotoPending {
		t.Fatal("foto baru pertama harus kembali pending")
	}
}

func TestEditDoesNotTouchIdentityFieldsAndReplayShowsNewVersion(t *testing.T) {
	r := newEditRig(t, false)
	p0 := r.uploaded(t, nil)
	req := in(p0)
	first, err := r.uc.Submit(bg, req)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := r.subs.Get(bg, first.SubmissionID)
	r.clock.t = r.clock.t.Add(time.Hour)
	if _, err := r.edit(first, 1, ep(p0, "diedit")); err != nil {
		t.Fatal(err)
	}
	after, _ := r.subs.Get(bg, first.SubmissionID)
	if after.ClientRequestID != before.ClientRequestID || !after.CreatedAt.Equal(before.CreatedAt) ||
		after.OwnerID != before.OwnerID || after.ID != before.ID {
		t.Fatalf("edit tidak boleh mengubah identitas: %+v → %+v", before, after)
	}
	// POST yang diulang (jaringan) setelah edit: versi terkini, tanpa menimpa hasil edit
	again, err := r.uc.Submit(bg, req)
	if err != nil || !again.Replayed || again.Version != 2 {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if got := r.detail(t, first.SubmissionID).Photos[0].Annotations[0].Text; got != "diedit" {
		t.Fatalf("replay tidak boleh menimpa hasil edit: %q", got)
	}
}
