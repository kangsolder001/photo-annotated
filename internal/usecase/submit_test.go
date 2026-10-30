package usecase_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/imgfix"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

const editSecret = "fedcba9876543210fedcba9876543210fedcba9876543210"

type subRig struct {
	uc     *usecase.Submit
	pool   *pgxpool.Pool
	photos *postgres.PhotoRepo
	subs   *postgres.SubmissionRepo
	store  *localfs.Store
	dir    string
	clock  *fakeClock
	edit   *signer.EditTokens
	labels seedLabels
}

type rigOpts struct {
	maxPhotos int
	noEdit    bool
	photoRepo func(usecase.PhotoRepo) usecase.PhotoRepo
	store     func(usecase.PhotoStore) usecase.PhotoStore
}

func newSubRig(t *testing.T, o rigOpts) *subRig {
	t.Helper()
	pool := pgtest.Pool(t)
	dir := t.TempDir()
	store, err := localfs.New(localfs.Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	et, err := signer.NewEditTokens(editSecret)
	if err != nil {
		t.Fatal(err)
	}
	if o.maxPhotos == 0 {
		o.maxPhotos = 5
	}
	r := &subRig{
		pool: pool, photos: postgres.NewPhotoRepo(pool), subs: postgres.NewSubmissionRepo(pool),
		store: store, dir: dir, clock: &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}, edit: et,
	}
	var pr usecase.PhotoRepo = r.photos
	if o.photoRepo != nil {
		pr = o.photoRepo(pr)
	}
	var ps usecase.PhotoStore = store
	if o.store != nil {
		ps = o.store(ps)
	}
	var tok usecase.EditTokenSigner = et
	if o.noEdit {
		tok = nil
	}
	r.labels = newSeedLabels(t, pool)
	r.uc = usecase.NewSubmit(pr, r.subs, r.labels, postgres.NewTxManager(pool), ps, tok, r.clock,
		usecase.SubmitConfig{MaxPhotos: o.maxPhotos, MaxAnnotationsPerPhoto: 50})
	return r
}

// uploaded membuat foto pending yang sudah benar-benar ter-upload (file di disk + baris DB).
func (r *subRig) uploaded(t *testing.T, owner *uuid.UUID) *domain.Photo {
	t.Helper()
	id := uuid.New()
	key, err := domain.NewStorageKey(id, r.clock.t, domain.ContentTypeWebP)
	if err != nil {
		t.Fatal(err)
	}
	body := imgfix.WebP(100, 80, 2000, byte(id[0]))
	p := &domain.Photo{ID: id, OwnerID: owner, StorageKey: key, ContentType: domain.ContentTypeWebP,
		SizeBytes: int64(len(body)), Width: 100, Height: 80, Status: domain.PhotoPending, CreatedAt: r.clock.t}
	if err := r.photos.Create(bg, p); err != nil {
		t.Fatal(err)
	}
	st, err := r.store.Stage(bg, key, bytes.NewReader(body), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := r.photos.MarkUploaded(bg, id, r.clock.t, st.SHA256()); err != nil {
		t.Fatal(err)
	}
	return p
}

// pendingOnly membuat foto yang BELUM di-upload.
func (r *subRig) pendingOnly(t *testing.T) *domain.Photo {
	t.Helper()
	id := uuid.New()
	key, _ := domain.NewStorageKey(id, r.clock.t, domain.ContentTypeWebP)
	p := &domain.Photo{ID: id, StorageKey: key, ContentType: domain.ContentTypeWebP, SizeBytes: 2000, Width: 100, Height: 80,
		Status: domain.PhotoPending, CreatedAt: r.clock.t}
	if err := r.photos.Create(bg, p); err != nil {
		t.Fatal(err)
	}
	return p
}

// Label uji: nama → id tetap (mulai 100001, jauh dari id yang dibuat identity). Rig memasang seedLabels
// di depan LabelRepo asli sehingga label yang dipakai test sudah ada saat divalidasi; validasi label
// sendiri (FOR SHARE, ada/aktif, pengecualian arsip saat edit) tetap berjalan pada repo dan transaksi
// yang asli. Id yang tidak terdaftar (mis. 999999) tidak dibuat, sehingga tetap "tidak ada".
// Penyemaian memakai satu koneksi khusus (bukan pool): ForShare dipanggil di dalam transaksi yang
// sudah memegang koneksi, jadi meminjam koneksi lain dari pool bisa macet saat test konkurensi.
var (
	testLabelsMu   sync.Mutex
	testLabelIDs   = map[string]int{}
	testLabelNames = map[int]string{}
)

func lbl(name string) int {
	testLabelsMu.Lock()
	defer testLabelsMu.Unlock()
	if id, ok := testLabelIDs[name]; ok {
		return id
	}
	id := 100001 + len(testLabelIDs)
	testLabelIDs[name], testLabelNames[id] = id, name
	return id
}

type seedLabels struct {
	*postgres.LabelRepo
	mu   *sync.Mutex
	conn *pgxpool.Conn
}

func newSeedLabels(t *testing.T, pool *pgxpool.Pool) seedLabels {
	t.Helper()
	conn, err := pool.Acquire(bg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release) // berjalan sebelum pool ditutup (Cleanup LIFO)
	return seedLabels{LabelRepo: postgres.NewLabelRepo(pool), mu: &sync.Mutex{}, conn: conn}
}

func (s seedLabels) ForShare(ctx context.Context, ids []int) (map[int]domain.Label, error) {
	s.mu.Lock()
	for _, id := range ids {
		testLabelsMu.Lock()
		name, ok := testLabelNames[id]
		testLabelsMu.Unlock()
		if !ok {
			continue
		}
		if _, err := s.conn.Exec(context.WithoutCancel(ctx), `INSERT INTO labels (id, name, color) OVERRIDING SYSTEM VALUE
			VALUES ($1, $2, '#123456') ON CONFLICT DO NOTHING`, id, name); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	}
	s.mu.Unlock()
	return s.LabelRepo.ForShare(ctx, ids)
}

// labelName: nama label kotak yang dibaca dari DB ("" untuk baris lama tanpa label).
func labelName(a domain.Annotation) string {
	if a.Label == nil {
		return ""
	}
	return a.Label.Name
}

func ann(name string) domain.Annotation {
	return domain.Annotation{LabelID: lbl(name), X: 0.1, Y: 0.1, W: 0.2, H: 0.2}
}

func in(photos ...*domain.Photo) usecase.SubmitInput {
	si := usecase.SubmitInput{FormVersion: 1, Answers: surveyAnswers(true), ClientRequestID: uuid.New()}
	for _, p := range photos {
		si.Photos = append(si.Photos, usecase.SubmitPhoto{PhotoID: p.ID, Annotations: []domain.Annotation{ann("catatan")}})
	}
	return si
}

func (r *subRig) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := r.pool.QueryRow(bg, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (r *subRig) photoStatus(t *testing.T, p *domain.Photo) domain.PhotoStatus {
	t.Helper()
	got, err := r.photos.Get(bg, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if codeOf(err) != code {
		t.Fatalf("err = %v, want kode %s", err, code)
	}
}

// ---------------------------------------------------------------- sukses

func TestSubmitSuccessSinglePhoto(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	p := r.uploaded(t, nil)
	ip := netip.MustParseAddr("203.0.113.9")
	req := usecase.SubmitInput{FormVersion: 1, Answers: surveyAnswers(true),
		ClientRequestID: uuid.New(), ClientIP: &ip,
		Photos: []usecase.SubmitPhoto{{PhotoID: p.ID, Annotations: []domain.Annotation{
			{LabelID: lbl("halo dunia"), Text: "diabaikan", X: 0.12, Y: 0.3, W: 0.25, H: 0.18},
			{LabelID: lbl("kedua"), X: 0.55, Y: 0.1, W: 0.3, H: 0.12},
		}}},
	}
	out, err := r.uc.Submit(bg, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Replayed || out.Version != 1 || !out.CreatedAt.Equal(r.clock.t) || out.SubmissionID.Version() != 7 {
		t.Fatalf("output: %+v", out)
	}
	if out.EditToken == "" || !r.edit.Verify(out.SubmissionID, out.EditToken) {
		t.Fatalf("edit token tidak sah: %q", out.EditToken)
	}

	d, err := r.subs.GetDetail(bg, out.SubmissionID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Submission.ClientRequestID != req.ClientRequestID || d.Submission.OwnerID != nil ||
		d.Submission.ClientIP == nil || *d.Submission.ClientIP != ip || d.Submission.Version != 1 {
		t.Fatalf("submission tersimpan: %+v", d.Submission)
	}
	if len(d.Photos) != 1 || d.Photos[0].Photo.ID != p.ID || d.Photos[0].Photo.Status != domain.PhotoAttached || d.Photos[0].Photo.Position != 0 {
		t.Fatalf("foto: %+v", d.Photos)
	}
	got := d.Photos[0].Annotations
	if len(got) != 2 || got[0].LabelID != lbl("halo dunia") || labelName(got[0]) != "halo dunia" || got[0].Text != "" ||
		got[0].X != 0.12 || labelName(got[1]) != "kedua" || got[1].Label.Color != "#123456" || got[1].Label.Archived() {
		t.Fatalf("anotasi (label lengkap, tanpa teks): %+v", got)
	}
}

func TestSubmitMultiplePhotosKeepRequestOrder(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	a, b, c := r.uploaded(t, nil), r.uploaded(t, nil), r.uploaded(t, nil)
	req := in(c, a, b) // urutan tampil = urutan request
	req.Photos[1].Annotations = nil
	out, err := r.uc.Submit(bg, req)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := r.subs.GetDetail(bg, out.SubmissionID)
	if len(d.Photos) != 3 || d.Photos[0].Photo.ID != c.ID || d.Photos[1].Photo.ID != a.ID || d.Photos[2].Photo.ID != b.ID {
		t.Fatalf("urutan foto salah: %+v", d.Photos)
	}
	for i, ph := range d.Photos {
		if ph.Photo.Position != i {
			t.Fatalf("position[%d] = %d", i, ph.Photo.Position)
		}
	}
	if len(d.Photos[1].Annotations) != 0 || len(d.Photos[0].Annotations) != 1 {
		t.Fatalf("anotasi per foto salah: %+v", d.Photos)
	}
}

func TestSubmitPhotoWithZeroAnnotationsAndBoundary50(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	a, b := r.uploaded(t, nil), r.uploaded(t, nil)
	req := in(a, b)
	req.Photos[0].Annotations = nil
	req.Photos[1].Annotations = make([]domain.Annotation, 50)
	for i := range req.Photos[1].Annotations {
		req.Photos[1].Annotations[i] = ann("n")
	}
	if _, err := r.uc.Submit(bg, req); err != nil {
		t.Fatalf("0 dan 50 anotasi harus diterima: %v", err)
	}
}

func TestSubmitWithoutEditTokenSigner(t *testing.T) {
	r := newSubRig(t, rigOpts{noEdit: true})
	out, err := r.uc.Submit(bg, in(r.uploaded(t, nil)))
	if err != nil || out.EditToken != "" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// ---------------------------------------------------------------- validasi bentuk

func TestSubmitShapeValidation(t *testing.T) {
	r := newSubRig(t, rigOpts{maxPhotos: 3})
	ok := r.uploaded(t, nil)
	dupID := r.uploaded(t, nil)

	mk := func(mut func(*usecase.SubmitInput)) usecase.SubmitInput { s := in(ok); mut(&s); return s }
	tooMany := make([]domain.Annotation, 51)
	for i := range tooMany {
		tooMany[i] = ann("x")
	}
	cases := []struct {
		name string
		in   usecase.SubmitInput
		code string
		msg  string
	}{
		{"client_request_id nol", mk(func(s *usecase.SubmitInput) { s.ClientRequestID = uuid.Nil }), "INVALID_CLIENT_REQUEST_ID", ""},
		{"tanpa foto", mk(func(s *usecase.SubmitInput) { s.Photos = nil }), "INVALID_PHOTO_COUNT", ""},
		{"melebihi batas foto", in(ok, ok, ok, ok), "INVALID_PHOTO_COUNT", ""},
		{"foto kembar", in(dupID, dupID), "DUPLICATE_PHOTO", "photos[1]"},
		{"photo_id nol", mk(func(s *usecase.SubmitInput) { s.Photos[0].PhotoID = uuid.Nil }), "PHOTO_NOT_FOUND", "photos[0]"},
		{"51 anotasi", mk(func(s *usecase.SubmitInput) { s.Photos[0].Annotations = tooMany }), "TOO_MANY_ANNOTATIONS", "photos[0]"},
		{"bbox salah", mk(func(s *usecase.SubmitInput) {
			s.Photos[0].Annotations = []domain.Annotation{ann("ok"), {LabelID: lbl("x"), X: 0.9, Y: 0, W: 0.5, H: 0.5}}
		}), "INVALID_BBOX", "photos[0].annotations[1]"},
		{"tanpa label_id", mk(func(s *usecase.SubmitInput) {
			s.Photos[0].Annotations = []domain.Annotation{ann("ok"), {Text: "teks bebas", X: 0.1, Y: 0.1, W: 0.2, H: 0.2}}
		}), "INVALID_LABEL", "photos[0].annotations[1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.uc.Submit(bg, tc.in)
			de, isDomain := domain.AsError(err)
			if !isDomain || de.Code != tc.code || de.Kind != domain.KindInvalid {
				t.Fatalf("err = %v, want %s/Invalid", err, tc.code)
			}
			if tc.msg != "" && !strings.Contains(de.Message, tc.msg) {
				t.Fatalf("pesan %q harus memuat lokasi %q", de.Message, tc.msg)
			}
		})
	}
	if r.count(t, "submissions") != 0 {
		t.Fatal("validasi gagal tidak boleh membuat submission")
	}
	if r.photoStatus(t, ok) != domain.PhotoPending {
		t.Fatal("foto harus tetap pending")
	}
}

// ---------------------------------------------------------------- status & kepemilikan foto

func TestSubmitPhotoStateErrors(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	t.Run("foto tidak ada", func(t *testing.T) {
		ghost := &domain.Photo{ID: uuid.New()}
		_, err := r.uc.Submit(bg, in(ghost))
		wantCode(t, err, "PHOTO_NOT_FOUND")
	})
	t.Run("belum ter-upload", func(t *testing.T) {
		p := r.pendingOnly(t)
		_, err := r.uc.Submit(bg, in(p))
		de, ok := domain.AsError(err)
		if !ok || de.Code != "PHOTO_NOT_UPLOADED" || de.Kind != domain.KindInvalid || !strings.Contains(de.Message, "photos[0]") {
			t.Fatalf("err = %v", err)
		}
		if r.photoStatus(t, p) != domain.PhotoPending {
			t.Fatal("foto harus tetap pending agar bisa di-upload")
		}
	})
	t.Run("sudah dipakai submission lain", func(t *testing.T) {
		p := r.uploaded(t, nil)
		if _, err := r.uc.Submit(bg, in(p)); err != nil {
			t.Fatal(err)
		}
		_, err := r.uc.Submit(bg, in(p))
		de, ok := domain.AsError(err)
		if !ok || de.Code != "PHOTO_NOT_AVAILABLE" || de.Kind != domain.KindConflict {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("rejected dan deleted", func(t *testing.T) {
		for _, st := range []string{"rejected", "deleted"} {
			p := r.uploaded(t, nil)
			if _, err := r.pool.Exec(bg, `UPDATE photos SET status=$2 WHERE id=$1`, p.ID, st); err != nil {
				t.Fatal(err)
			}
			_, err := r.uc.Submit(bg, in(p))
			wantCode(t, err, "PHOTO_NOT_AVAILABLE")
		}
	})
}

func TestSubmitPhotoOwnership(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	alice, bob := pgtest.CreateUser(t, r.pool), pgtest.CreateUser(t, r.pool)
	alicePhoto, anonPhoto := r.uploaded(t, &alice), r.uploaded(t, nil)

	// pemilik lain, anonim memakai foto milik user, dan user memakai foto anonim: semuanya
	// dijawab "tidak ada" agar keberadaan foto orang lain tidak bisa ditebak
	for name, c := range map[string]struct {
		owner *uuid.UUID
		photo *domain.Photo
	}{
		"bob memakai foto alice":    {&bob, alicePhoto},
		"anonim memakai foto alice": {nil, alicePhoto},
		"alice memakai foto anonim": {&alice, anonPhoto},
	} {
		req := in(c.photo)
		req.OwnerID = c.owner
		_, err := r.uc.Submit(bg, req)
		de, ok := domain.AsError(err)
		if !ok || de.Code != "PHOTO_NOT_FOUND" || de.Kind != domain.KindInvalid {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// pemilik sendiri boleh
	own := in(alicePhoto)
	own.OwnerID = &alice
	out, err := r.uc.Submit(bg, own)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := r.subs.Get(bg, out.SubmissionID)
	if got.OwnerID == nil || *got.OwnerID != alice {
		t.Fatalf("owner submission = %v", got.OwnerID)
	}
}

// ---------------------------------------------------------------- file di disk

func TestSubmitDetectsMissingOrCorruptFiles(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, r *subRig, p *domain.Photo){
		"file hilang": func(t *testing.T, r *subRig, p *domain.Photo) {
			if err := os.Remove(filepath.Join(r.dir, p.StorageKey)); err != nil {
				t.Fatal(err)
			}
		},
		"ukuran berubah": func(t *testing.T, r *subRig, p *domain.Photo) {
			if err := os.WriteFile(filepath.Join(r.dir, p.StorageKey), []byte("terpotong"), 0o640); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newSubRig(t, rigOpts{})
			good, bad := r.uploaded(t, nil), r.uploaded(t, nil)
			damage(t, r, bad)
			_, err := r.uc.Submit(bg, in(good, bad))
			de, ok := domain.AsError(err)
			if !ok || de.Code != "PHOTO_FILE_INVALID" || de.Kind != domain.KindInvalid || !strings.Contains(de.Message, "photos[1]") {
				t.Fatalf("err = %v", err)
			}
			if r.photoStatus(t, bad) != domain.PhotoRejected {
				t.Fatal("foto bermasalah harus rejected")
			}
			if r.photoStatus(t, good) != domain.PhotoPending {
				t.Fatal("foto yang sehat tidak boleh tersentuh")
			}
			if r.count(t, "submissions") != 0 || r.count(t, "annotations") != 0 {
				t.Fatal("tidak boleh ada submission/anotasi")
			}
		})
	}
}

type statErrStore struct {
	usecase.PhotoStore
	err error
}

func (s statErrStore) Stat(context.Context, string) (usecase.FileInfo, error) {
	return usecase.FileInfo{}, s.err
}

func TestSubmitStorageFailureIsNotThePhotosFault(t *testing.T) {
	boom := domain.Unavailable("STORAGE_UNAVAILABLE", "photo storage is unavailable")
	r := newSubRig(t, rigOpts{store: func(s usecase.PhotoStore) usecase.PhotoStore { return statErrStore{s, boom} }})
	p := r.uploaded(t, nil)
	_, err := r.uc.Submit(bg, in(p))
	wantCode(t, err, "STORAGE_UNAVAILABLE")
	if r.photoStatus(t, p) != domain.PhotoPending {
		t.Fatal("kegagalan penyimpanan tidak boleh menandai foto rejected")
	}
}

// ---------------------------------------------------------------- idempotensi

func TestSubmitIdempotentReplay(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	p := r.uploaded(t, nil)
	req := in(p)
	r.clock.t = r.clock.t.Add(123456789 * time.Nanosecond) // nanodetik: respons pertama dan replay harus tetap identik
	first, err := r.uc.Submit(bg, req)
	if err != nil {
		t.Fatal(err)
	}
	// edit di antaranya: replay harus memberi versi TERKINI (dibutuhkan PUT berikutnya)
	if _, err := r.subs.BumpVersion(bg, first.SubmissionID, 1, r.clock.t.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	again, err := r.uc.Submit(bg, req)
	if err != nil {
		t.Fatal(err)
	}
	if !again.CreatedAt.Equal(first.CreatedAt) || again.CreatedAt.Nanosecond() != 123456000 {
		t.Fatalf("created_at harus identik dan berpresisi mikrodetik: %v vs %v", first.CreatedAt, again.CreatedAt)
	}
	if !again.Replayed || again.SubmissionID != first.SubmissionID || again.EditToken != first.EditToken || again.Version != 2 {
		t.Fatalf("replay: %+v vs %+v", again, first)
	}
	if r.count(t, "submissions") != 1 || r.count(t, "annotations") != 1 {
		t.Fatal("replay tidak boleh membuat baris baru")
	}
	// replay tidak memeriksa ulang foto (yang sudah attached): tetap sukses walau payload dikirim ulang
	if r.photoStatus(t, p) != domain.PhotoAttached {
		t.Fatal("foto tetap attached")
	}
}

func TestSubmitReplayByAnotherOwnerIsRefused(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	alice, bob := pgtest.CreateUser(t, r.pool), pgtest.CreateUser(t, r.pool)
	req := in(r.uploaded(t, &alice))
	req.OwnerID = &alice
	if _, err := r.uc.Submit(bg, req); err != nil {
		t.Fatal(err)
	}
	for name, owner := range map[string]*uuid.UUID{"bob": &bob, "anonim": nil} {
		stolen := req
		stolen.OwnerID = owner
		_, err := r.uc.Submit(bg, stolen)
		de, ok := domain.AsError(err)
		if !ok || de.Code != "DUPLICATE_CLIENT_REQUEST" || de.Kind != domain.KindConflict {
			t.Fatalf("%s: err = %v (tidak boleh membocorkan submission alice)", name, err)
		}
	}
}

func TestSubmitConcurrentSameRequestCreatesExactlyOne(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	p1, p2 := r.uploaded(t, nil), r.uploaded(t, nil)
	req := in(p1, p2)
	const n = 10
	var wg sync.WaitGroup
	outs := make([]*usecase.SubmitOutput, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], errs[i] = r.uc.Submit(bg, req)
		}()
	}
	wg.Wait()
	created, id := 0, uuid.Nil
	for i := range outs {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if !outs[i].Replayed {
			created++
		}
		if id == uuid.Nil {
			id = outs[i].SubmissionID
		} else if outs[i].SubmissionID != id {
			t.Fatalf("semua jawaban harus untuk submission yang sama")
		}
	}
	if created != 1 {
		t.Fatalf("yang membuat = %d, want tepat 1", created)
	}
	if r.count(t, "submissions") != 1 || r.count(t, "annotations") != 2 {
		t.Fatalf("submissions=%d annotations=%d", r.count(t, "submissions"), r.count(t, "annotations"))
	}
}

func TestSubmitRacingForOnePhotoExactlyOneWins(t *testing.T) {
	r := newSubRig(t, rigOpts{})
	p := r.uploaded(t, nil)
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.uc.Submit(bg, in(p)) // client_request_id berbeda tiap request
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case codeOf(err) == "PHOTO_NOT_AVAILABLE":
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
	if r.count(t, "submissions") != 1 || r.count(t, "annotations") != 1 {
		t.Fatalf("pemenang saja yang boleh meninggalkan baris: submissions=%d annotations=%d",
			r.count(t, "submissions"), r.count(t, "annotations"))
	}
}

// ---------------------------------------------------------------- atomisitas

// flipOnSecondAttach membuat Attach kedua gagal di tengah transaksi (foto "diambil" pihak lain
// setelah pemeriksaan awal), untuk membuktikan seluruh transaksi dibatalkan.
type flipOnSecondAttach struct {
	usecase.PhotoRepo
	pool  *pgxpool.Pool
	calls int
}

func (f *flipOnSecondAttach) Attach(ctx context.Context, photoID, submissionID uuid.UUID, position int, owner *uuid.UUID) error {
	f.calls++
	if f.calls == 2 {
		if _, err := f.pool.Exec(context.Background(), `UPDATE photos SET status='deleted' WHERE id=$1`, photoID); err != nil {
			return err
		}
	}
	return f.PhotoRepo.Attach(ctx, photoID, submissionID, position, owner)
}

func TestSubmitIsAtomicWhenALaterStepFails(t *testing.T) {
	var flip *flipOnSecondAttach
	r := newSubRig(t, rigOpts{photoRepo: func(pr usecase.PhotoRepo) usecase.PhotoRepo { return pr }})
	// pasang pembungkus setelah pool tersedia
	flip = &flipOnSecondAttach{PhotoRepo: r.photos, pool: r.pool}
	et, _ := signer.NewEditTokens(editSecret)
	r.uc = usecase.NewSubmit(flip, r.subs, r.labels, postgres.NewTxManager(r.pool), r.store, et, r.clock,
		usecase.SubmitConfig{MaxPhotos: 5, MaxAnnotationsPerPhoto: 50})

	a, b := r.uploaded(t, nil), r.uploaded(t, nil)
	_, err := r.uc.Submit(bg, in(a, b))
	wantCode(t, err, "PHOTO_NOT_AVAILABLE")
	if r.count(t, "submissions") != 0 || r.count(t, "annotations") != 0 {
		t.Fatalf("rollback harus menghapus submission dan anotasi: sub=%d ann=%d", r.count(t, "submissions"), r.count(t, "annotations"))
	}
	if r.photoStatus(t, a) != domain.PhotoPending {
		t.Fatalf("foto pertama harus kembali pending, status %s", r.photoStatus(t, a))
	}
	// setelah gagal, request yang sama dengan foto sehat bisa dicoba lagi
	c := r.uploaded(t, nil)
	retry := in(a, c)
	if _, err := r.uc.Submit(bg, retry); err != nil {
		t.Fatalf("ulang setelah rollback: %v", err)
	}
}

func TestNewSubmitUsesErrorsIsForDuplicate(t *testing.T) {
	// guard ringan: sentinel duplikat dari repo dikenali lewat errors.Is berdasarkan Code
	err := domain.Conflict("DUPLICATE_CLIENT_REQUEST", "x")
	if !errors.Is(err, domain.Conflict("DUPLICATE_CLIENT_REQUEST", "lain")) {
		t.Fatal("errors.Is harus cocok berdasarkan Code")
	}
}

// surveyAnswers membuat jawaban pendataan versi 1 yang sah (spec-pendataan §4). consent false = menolak
// (hanya bagian A).
func surveyAnswers(consent bool) map[string]json.RawMessage {
	m := map[string]any{"a_date": "2020-01-02", "a_rw": "05", "a_rt": "03", "a_household_code": "RT03-RW05-12",
		"a_initials": "Ny. S", "a_respondent": "head", "a_consent": "no"}
	if consent {
		m["a_consent"] = "yes"
		for i := 1; i <= 30; i++ {
			m[fmt.Sprintf("b%d", i)] = "yes"
		}
		for i := 1; i <= 10; i++ {
			m[fmt.Sprintf("c%d", i)] = "na"
		}
		m["r3_sorting"], m["r3_visible"] = "partial", "clear"
	}
	out := map[string]json.RawMessage{}
	for k, v := range m {
		out[k], _ = json.Marshal(v)
	}
	return out
}
