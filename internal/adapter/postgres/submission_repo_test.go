package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
)

var ctx = context.Background()

type rig struct {
	pool   *pgxpool.Pool
	photos *postgres.PhotoRepo
	subs   *postgres.SubmissionRepo
	tx     *postgres.TxManager
}

func newRig(t *testing.T) *rig {
	t.Helper()
	pool := pgtest.Pool(t)
	return &rig{pool: pool, photos: postgres.NewPhotoRepo(pool), subs: postgres.NewSubmissionRepo(pool), tx: postgres.NewTxManager(pool)}
}

func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// uploadedPhoto membuat foto pending yang sudah ter-upload (siap dipakai submission).
func (r *rig) uploadedPhoto(t *testing.T, owner *uuid.UUID) *domain.Photo {
	t.Helper()
	p := validPhoto()
	p.OwnerID = owner
	if err := r.photos.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := r.photos.MarkUploaded(ctx, p.ID, now(), bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	return p
}

func newSub(owner *uuid.UUID) *domain.Submission {
	ip := netip.MustParseAddr("203.0.113.9")
	return &domain.Submission{
		ID: uuid.New(), OwnerID: owner, ClientRequestID: uuid.New(), ClientIP: &ip,
		Version: 1, CreatedAt: now(), UpdatedAt: now(),
	}
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func codeOf(err error) string {
	if de, ok := domain.AsError(err); ok {
		return de.Code
	}
	return ""
}

// ---------------------------------------------------------------- submission CRUD

func TestSubmissionCreateGet(t *testing.T) {
	r := newRig(t)
	owner := pgtest.CreateUser(t, r.pool)
	s := newSub(&owner)
	if err := r.subs.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	for name, get := range map[string]func() (*domain.Submission, error){
		"Get":                  func() (*domain.Submission, error) { return r.subs.Get(ctx, s.ID) },
		"GetByClientRequestID": func() (*domain.Submission, error) { return r.subs.GetByClientRequestID(ctx, s.ClientRequestID) },
	} {
		got, err := get()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.ID != s.ID || got.ClientRequestID != s.ClientRequestID || got.Version != 1 ||
			got.OwnerID == nil || *got.OwnerID != owner || got.ClientIP == nil || *got.ClientIP != *s.ClientIP ||
			!got.CreatedAt.Equal(s.CreatedAt) || !got.UpdatedAt.Equal(s.UpdatedAt) {
			t.Fatalf("%s salah:\n got %+v\nwant %+v", name, got, s)
		}
	}
	// pemilik dan IP opsional
	anon := newSub(nil)
	anon.ClientIP = nil
	if err := r.subs.Create(ctx, anon); err != nil {
		t.Fatal(err)
	}
	got, _ := r.subs.Get(ctx, anon.ID)
	if got.OwnerID != nil || got.ClientIP != nil {
		t.Fatalf("owner/ip harus NULL: %+v", got)
	}
}

func TestSubmissionNotFound(t *testing.T) {
	r := newRig(t)
	if _, err := r.subs.Get(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get: %v", err)
	}
	if _, err := r.subs.GetByClientRequestID(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByClientRequestID: %v", err)
	}
	if _, err := r.subs.GetDetail(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetDetail: %v", err)
	}
	if _, err := r.subs.BumpVersion(ctx, uuid.New(), 1, now()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("BumpVersion: %v", err)
	}
}

func TestSubmissionClientRequestIDIsUnique(t *testing.T) {
	r := newRig(t)
	a := newSub(nil)
	b := newSub(nil)
	b.ClientRequestID = a.ClientRequestID
	if err := r.subs.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	err := r.subs.Create(ctx, b)
	de, ok := domain.AsError(err)
	if !ok || de.Code != "DUPLICATE_CLIENT_REQUEST" || de.Kind != domain.KindConflict {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmissionVersionConstraint(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	s.Version = 0
	if err := r.subs.Create(ctx, s); pgCode(err) != "23514" {
		t.Fatalf("version 0 harus ditolak CHECK: %v", err)
	}
}

// ---------------------------------------------------------------- detail

func TestGetDetailGroupsPhotosAndAnnotationsInOrder(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	if err := r.subs.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	p1, p2, p3 := r.uploadedPhoto(t, nil), r.uploadedPhoto(t, nil), r.uploadedPhoto(t, nil)
	// sengaja dilampirkan tidak berurutan: urutan tampil mengikuti position, bukan urutan attach
	for _, c := range []struct {
		p   *domain.Photo
		pos int
	}{{p3, 2}, {p1, 0}, {p2, 1}} {
		if err := r.photos.Attach(ctx, c.p.ID, s.ID, c.pos, nil); err != nil {
			t.Fatal(err)
		}
	}
	a1 := []domain.Annotation{
		{Text: "pertama", X: 0.12, Y: 0.3, W: 0.25, H: 0.18},
		{Text: "kedua\nbaris", X: 0.55, Y: 0.1, W: 0.3, H: 0.12},
		{Text: "ketiga 日本語", X: 0, Y: 0, W: 1, H: 1},
	}
	a3 := []domain.Annotation{{Text: "di foto 3", X: 0.5, Y: 0.5, W: 0.5, H: 0.5}}
	if err := r.subs.AddAnnotations(ctx, p1.ID, a1); err != nil {
		t.Fatal(err)
	}
	if err := r.subs.AddAnnotations(ctx, p3.ID, a3); err != nil {
		t.Fatal(err)
	}
	// foto yang dihapus tidak ikut
	p4 := r.uploadedPhoto(t, nil)
	if err := r.photos.Attach(ctx, p4.ID, s.ID, 3, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, `UPDATE photos SET status='deleted' WHERE id=$1`, p4.ID); err != nil {
		t.Fatal(err)
	}

	d, err := r.subs.GetDetail(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Submission.ID != s.ID || d.Submission.Version != 1 {
		t.Fatalf("submission: %+v", d.Submission)
	}
	if len(d.Photos) != 3 {
		t.Fatalf("jumlah foto = %d, want 3 (yang dihapus tidak ikut)", len(d.Photos))
	}
	for i, want := range []*domain.Photo{p1, p2, p3} {
		got := d.Photos[i].Photo
		if got.ID != want.ID || got.Position != i || got.Status != domain.PhotoAttached ||
			got.SubmissionID == nil || *got.SubmissionID != s.ID || got.UploadedAt == nil || len(got.SHA256) != 32 {
			t.Fatalf("foto[%d] salah: %+v", i, got)
		}
	}
	// anotasi: urutan pembuatan dipertahankan, nilai float64 kembali persis sama
	if got := d.Photos[0].Annotations; len(got) != 3 || got[0] != a1[0] || got[1] != a1[1] || got[2] != a1[2] {
		t.Fatalf("anotasi foto 1: %+v", got)
	}
	if got := d.Photos[1].Annotations; got == nil || len(got) != 0 {
		t.Fatalf("foto tanpa anotasi harus slice kosong (bukan nil): %#v", got)
	}
	if got := d.Photos[2].Annotations; len(got) != 1 || got[0] != a3[0] {
		t.Fatalf("anotasi foto 3: %+v", got)
	}
}

func TestGetDetailSubmissionWithoutPhotos(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	if err := r.subs.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	d, err := r.subs.GetDetail(ctx, s.ID)
	if err != nil || d.Photos == nil || len(d.Photos) != 0 {
		t.Fatalf("detail = %+v err=%v", d, err)
	}
}

func TestAnnotationsPreserveFloat64Exactly(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	p := r.uploadedPhoto(t, nil)
	if err := r.photos.Attach(ctx, p.ID, s.ID, 0, nil); err != nil {
		t.Fatal(err)
	}
	in := domain.Annotation{Text: "presisi", X: 0.12, Y: 0.1 + 0.2, W: 1.0 / 3.0, H: 0.005}
	if err := r.subs.AddAnnotations(ctx, p.ID, []domain.Annotation{in}); err != nil {
		t.Fatal(err)
	}
	d, _ := r.subs.GetDetail(ctx, s.ID)
	if got := d.Photos[0].Annotations[0]; got != in {
		t.Fatalf("float64 harus kembali persis (bukan real): got %+v want %+v", got, in)
	}
}

// ---------------------------------------------------------------- constraint anotasi

func TestAnnotationConstraints(t *testing.T) {
	r := newRig(t)
	p := r.uploadedPhoto(t, nil)
	long := make([]rune, 1001)
	for i := range long {
		long[i] = 'x'
	}
	eps := domain.BBoxEpsilon
	good := domain.Annotation{Text: "ok", X: 0.1, Y: 0.1, W: 0.2, H: 0.2}
	cases := []struct {
		name string
		a    domain.Annotation
		ok   bool
	}{
		{"valid", good, true},
		{"seluruh foto", domain.Annotation{Text: "a", X: 0, Y: 0, W: 1, H: 1}, true},
		{"x+w tepat 1+eps/2 (toleransi)", domain.Annotation{Text: "a", X: 0.7, Y: 0, W: 0.3 + eps/2, H: 1}, true},
		{"x+w melebihi toleransi", domain.Annotation{Text: "a", X: 0.7, Y: 0, W: 0.3 + 10*eps, H: 1}, false},
		{"y+h melebihi toleransi", domain.Annotation{Text: "a", X: 0, Y: 0.7, W: 1, H: 0.3 + 10*eps}, false},
		{"x = 1", domain.Annotation{Text: "a", X: 1, Y: 0, W: 0.5, H: 0.5}, false},
		{"x negatif", domain.Annotation{Text: "a", X: -0.1, Y: 0, W: 0.5, H: 0.5}, false},
		{"y = 1", domain.Annotation{Text: "a", X: 0, Y: 1, W: 0.5, H: 0.5}, false},
		{"w di bawah minimum", domain.Annotation{Text: "a", X: 0, Y: 0, W: 0.0049, H: 0.5}, false},
		{"h di bawah minimum", domain.Annotation{Text: "a", X: 0, Y: 0, W: 0.5, H: 0.0049}, false},
		{"w > 1", domain.Annotation{Text: "a", X: 0, Y: 0, W: 1.1, H: 0.5}, false},
		{"teks kosong", domain.Annotation{Text: "", X: 0, Y: 0, W: 0.5, H: 0.5}, false},
		{"teks 1001 karakter", domain.Annotation{Text: string(long), X: 0, Y: 0, W: 0.5, H: 0.5}, false},
		{"teks 1000 karakter", domain.Annotation{Text: string(long[:1000]), X: 0, Y: 0, W: 0.5, H: 0.5}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.subs.AddAnnotations(ctx, p.ID, []domain.Annotation{tc.a})
			if tc.ok && err != nil {
				t.Fatalf("seharusnya lolos: %v", err)
			}
			if !tc.ok && pgCode(err) != "23514" {
				t.Fatalf("seharusnya ditolak CHECK (23514): %v", err)
			}
		})
	}
	// semua kondisi di atas konsisten dengan validasi domain
	for _, tc := range cases {
		if tc.a.Text == "" || len(tc.a.Text) > 1000 {
			continue
		}
		if dom := tc.a.ValidateBBox() == nil; dom != tc.ok {
			t.Errorf("%s: domain=%v tetapi DB=%v — aturan domain dan CHECK harus selaras", tc.name, dom, tc.ok)
		}
	}
}

func TestAnnotationRequiresExistingPhoto(t *testing.T) {
	r := newRig(t)
	err := r.subs.AddAnnotations(ctx, uuid.New(), []domain.Annotation{{Text: "x", X: 0, Y: 0, W: 0.5, H: 0.5}})
	if pgCode(err) != "23503" {
		t.Fatalf("FK ke photos harus menolak: %v", err)
	}
}

func TestAddAnnotationsEmptyIsNoop(t *testing.T) {
	r := newRig(t)
	if err := r.subs.AddAnnotations(ctx, uuid.New(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestAddAnnotationsIsAtomicWithinBatch(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	p := r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, p.ID, s.ID, 0, nil)
	err := r.tx.WithTx(ctx, func(ctx context.Context) error {
		return r.subs.AddAnnotations(ctx, p.ID, []domain.Annotation{
			{Text: "valid", X: 0, Y: 0, W: 0.5, H: 0.5},
			{Text: "", X: 0, Y: 0, W: 0.5, H: 0.5}, // melanggar CHECK
		})
	})
	if pgCode(err) != "23514" {
		t.Fatalf("err = %v", err)
	}
	d, _ := r.subs.GetDetail(ctx, s.ID)
	if len(d.Photos[0].Annotations) != 0 {
		t.Fatalf("transaksi gagal tidak boleh meninggalkan anotasi: %+v", d.Photos[0].Annotations)
	}
}

// ---------------------------------------------------------------- cascade

func TestCascadeDeletes(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	p := r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, p.ID, s.ID, 0, nil)
	_ = r.subs.AddAnnotations(ctx, p.ID, []domain.Annotation{{Text: "x", X: 0, Y: 0, W: 0.5, H: 0.5}})

	if _, err := r.pool.Exec(ctx, `DELETE FROM submissions WHERE id=$1`, s.ID); err != nil {
		t.Fatal(err)
	}
	for table, q := range map[string]string{
		"photos":      `SELECT count(*) FROM photos WHERE id=$1`,
		"annotations": `SELECT count(*) FROM annotations WHERE photo_id=$1`,
	} {
		var n int
		if err := r.pool.QueryRow(ctx, q, p.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s harus ikut terhapus saat submission dihapus: n=%d err=%v", table, n, err)
		}
	}
}

func TestPhotoSubmissionForeignKey(t *testing.T) {
	r := newRig(t)
	p := validPhoto()
	bogus := uuid.New()
	p.SubmissionID = &bogus
	if err := r.photos.Create(ctx, p); pgCode(err) != "23503" {
		t.Fatalf("submission_id yang tak ada harus ditolak FK: %v", err)
	}
}

// ---------------------------------------------------------------- Attach

func TestAttachConditions(t *testing.T) {
	r := newRig(t)
	owner, other := pgtest.CreateUser(t, r.pool), pgtest.CreateUser(t, r.pool)
	s := newSub(&owner)
	_ = r.subs.Create(ctx, s)

	t.Run("sukses menyimpan posisi dan status", func(t *testing.T) {
		p := r.uploadedPhoto(t, &owner)
		if err := r.photos.Attach(ctx, p.ID, s.ID, 4, &owner); err != nil {
			t.Fatal(err)
		}
		got, _ := r.photos.Get(ctx, p.ID)
		if got.Status != domain.PhotoAttached || got.Position != 4 || got.SubmissionID == nil || *got.SubmissionID != s.ID {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("anonim (owner nil) cocok dengan owner nil", func(t *testing.T) {
		p := r.uploadedPhoto(t, nil)
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("kedua kali ditolak", func(t *testing.T) {
		p := r.uploadedPhoto(t, &owner)
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, &owner); err != nil {
			t.Fatal(err)
		}
		if err := r.photos.Attach(ctx, p.ID, s.ID, 1, &owner); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("belum ter-upload ditolak", func(t *testing.T) {
		p := validPhoto()
		p.OwnerID = &owner
		_ = r.photos.Create(ctx, p)
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, &owner); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("pemilik berbeda ditolak", func(t *testing.T) {
		p := r.uploadedPhoto(t, &owner)
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, &other); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
			t.Fatalf("err = %v", err)
		}
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, nil); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
			t.Fatalf("anonim tidak boleh memakai foto milik user: %v", err)
		}
	})
	t.Run("anonim tidak boleh dipakai user", func(t *testing.T) {
		p := r.uploadedPhoto(t, nil)
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, &owner); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("rejected dan deleted ditolak", func(t *testing.T) {
		for _, st := range []string{"rejected", "deleted"} {
			p := r.uploadedPhoto(t, &owner)
			if _, err := r.pool.Exec(ctx, `UPDATE photos SET status=$2 WHERE id=$1`, p.ID, st); err != nil {
				t.Fatal(err)
			}
			if err := r.photos.Attach(ctx, p.ID, s.ID, 0, &owner); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
				t.Fatalf("%s: %v", st, err)
			}
		}
	})
	t.Run("foto tak ada ditolak", func(t *testing.T) {
		if err := r.photos.Attach(ctx, uuid.New(), s.ID, 0, &owner); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
			t.Fatalf("err = %v", err)
		}
	})
}

// Dua submission berebut satu foto: tepat satu yang boleh menang (inilah pagar terhadap
// pemakaian ganda saat dua request datang bersamaan).
func TestAttachRaceExactlyOneWins(t *testing.T) {
	r := newRig(t)
	p := r.uploadedPhoto(t, nil)
	const n = 12
	subs := make([]*domain.Submission, n)
	for i := range subs {
		subs[i] = newSub(nil)
		if err := r.subs.Create(ctx, subs[i]); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, losses := 0, 0
	for i := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.tx.WithTx(ctx, func(ctx context.Context) error {
				return r.photos.Attach(ctx, p.ID, subs[i].ID, 0, nil)
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case codeOf(err) == "PHOTO_NOT_AVAILABLE":
				losses++
			default:
				t.Errorf("error tak terduga: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || losses != n-1 {
		t.Fatalf("menang=%d kalah=%d, want 1 dan %d", wins, losses, n-1)
	}
}

func TestListBySubmission(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	a, b, c := r.uploadedPhoto(t, nil), r.uploadedPhoto(t, nil), r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, b.ID, s.ID, 1, nil)
	_ = r.photos.Attach(ctx, a.ID, s.ID, 0, nil)
	_ = r.photos.Attach(ctx, c.ID, s.ID, 2, nil)
	_, _ = r.pool.Exec(ctx, `UPDATE photos SET status='deleted' WHERE id=$1`, c.ID)
	got, err := r.photos.ListBySubmission(ctx, s.ID)
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if got, err := r.photos.ListBySubmission(ctx, uuid.New()); err != nil || len(got) != 0 {
		t.Fatalf("submission tanpa foto: %v %v", got, err)
	}
}

// ---------------------------------------------------------------- MarkUploaded / MarkRejected

func TestMarkUploadedAndRejected(t *testing.T) {
	r := newRig(t)
	p := validPhoto()
	_ = r.photos.Create(ctx, p)
	at, sum := now(), bytes.Repeat([]byte{9}, 32)
	if err := r.photos.MarkUploaded(ctx, p.ID, at, sum); err != nil {
		t.Fatal(err)
	}
	got, _ := r.photos.Get(ctx, p.ID)
	if got.UploadedAt == nil || !got.UploadedAt.Equal(at) || !bytes.Equal(got.SHA256, sum) || got.Status != domain.PhotoPending {
		t.Fatalf("%+v", got)
	}
	// ulang (retry) memperbarui hash
	sum2 := bytes.Repeat([]byte{8}, 32)
	if err := r.photos.MarkUploaded(ctx, p.ID, now(), sum2); err != nil {
		t.Fatal(err)
	}
	got, _ = r.photos.Get(ctx, p.ID)
	if !bytes.Equal(got.SHA256, sum2) {
		t.Fatal("hash harus diperbarui")
	}
	// MarkRejected hanya berlaku untuk pending
	q := validPhoto()
	_ = r.photos.Create(ctx, q)
	if err := r.photos.MarkRejected(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.photos.Get(ctx, q.ID); got.Status != domain.PhotoRejected {
		t.Fatalf("status = %s", got.Status)
	}
	if err := r.photos.MarkUploaded(ctx, q.ID, now(), sum); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("MarkUploaded pada rejected: %v", err)
	}
	// MarkRejected pada foto attached tidak mengubahnya
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	ok := r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, ok.ID, s.ID, 0, nil)
	if err := r.photos.MarkRejected(ctx, ok.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.photos.Get(ctx, ok.ID); got.Status != domain.PhotoAttached {
		t.Fatalf("foto attached tidak boleh berubah: %s", got.Status)
	}
}

// ---------------------------------------------------------------- versi & anotasi (edit)

func TestBumpVersion(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	t1 := now().Add(time.Minute)
	v, err := r.subs.BumpVersion(ctx, s.ID, 1, t1)
	if err != nil || v != 2 {
		t.Fatalf("v=%d err=%v", v, err)
	}
	got, _ := r.subs.Get(ctx, s.ID)
	if got.Version != 2 || !got.UpdatedAt.Equal(t1) || !got.CreatedAt.Equal(s.CreatedAt) {
		t.Fatalf("%+v", got)
	}
	// versi basi → konflik, tidak mengubah apa pun
	if _, err := r.subs.BumpVersion(ctx, s.ID, 1, now()); codeOf(err) != "VERSION_CONFLICT" {
		t.Fatalf("err = %v", err)
	}
	if got, _ := r.subs.Get(ctx, s.ID); got.Version != 2 {
		t.Fatal("versi tidak boleh berubah saat konflik")
	}
}

func TestBumpVersionRaceExactlyOneWins(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.subs.BumpVersion(ctx, s.ID, 1, now()); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if codeOf(err) != "VERSION_CONFLICT" {
				t.Errorf("err = %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("dua edit dengan versi sama: menang=%d, want 1 (inilah 409 VERSION_CONFLICT)", wins)
	}
}

func TestReplaceAnnotations(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	_ = r.subs.Create(ctx, s)
	p := r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, p.ID, s.ID, 0, nil)
	old := []domain.Annotation{{Text: "lama 1", X: 0, Y: 0, W: 0.5, H: 0.5}, {Text: "lama 2", X: 0.5, Y: 0.5, W: 0.5, H: 0.5}}
	fresh := []domain.Annotation{{Text: "baru", X: 0.1, Y: 0.1, W: 0.2, H: 0.2}}
	_ = r.subs.AddAnnotations(ctx, p.ID, old)

	if err := r.subs.ReplaceAnnotations(ctx, p.ID, fresh); err != nil {
		t.Fatal(err)
	}
	d, _ := r.subs.GetDetail(ctx, s.ID)
	if got := d.Photos[0].Annotations; len(got) != 1 || got[0] != fresh[0] {
		t.Fatalf("got %+v", got)
	}
	// mengganti dengan kosong menghapus semuanya
	if err := r.subs.ReplaceAnnotations(ctx, p.ID, nil); err != nil {
		t.Fatal(err)
	}
	d, _ = r.subs.GetDetail(ctx, s.ID)
	if len(d.Photos[0].Annotations) != 0 {
		t.Fatalf("got %+v", d.Photos[0].Annotations)
	}
	// penggantian yang gagal (CHECK) tidak boleh menghapus anotasi lama
	_ = r.subs.AddAnnotations(ctx, p.ID, old)
	bad := []domain.Annotation{{Text: "ok", X: 0, Y: 0, W: 0.5, H: 0.5}, {Text: "", X: 0, Y: 0, W: 0.5, H: 0.5}}
	if err := r.subs.ReplaceAnnotations(ctx, p.ID, bad); pgCode(err) != "23514" {
		t.Fatalf("err = %v", err)
	}
	d, _ = r.subs.GetDetail(ctx, s.ID)
	if got := d.Photos[0].Annotations; len(got) != 2 || got[0] != old[0] {
		t.Fatalf("anotasi lama harus utuh bila penggantian gagal: %+v", got)
	}
}

// ---------------------------------------------------------------- TxManager

func TestTxCommitMakesAllWritesVisible(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	p := r.uploadedPhoto(t, nil)
	err := r.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := r.subs.Create(ctx, s); err != nil {
			return err
		}
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, nil); err != nil {
			return err
		}
		return r.subs.AddAnnotations(ctx, p.ID, []domain.Annotation{{Text: "x", X: 0, Y: 0, W: 0.5, H: 0.5}})
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.subs.GetDetail(ctx, s.ID)
	if err != nil || len(d.Photos) != 1 || len(d.Photos[0].Annotations) != 1 {
		t.Fatalf("detail setelah commit: %+v %v", d, err)
	}
}

func TestTxRollbackOnErrorLeavesNothing(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	p := r.uploadedPhoto(t, nil)
	boom := errors.New("gagal di tengah")
	err := r.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := r.subs.Create(ctx, s); err != nil {
			return err
		}
		if err := r.photos.Attach(ctx, p.ID, s.ID, 0, nil); err != nil {
			return err
		}
		if err := r.subs.AddAnnotations(ctx, p.ID, []domain.Annotation{{Text: "x", X: 0, Y: 0, W: 0.5, H: 0.5}}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.subs.Get(ctx, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("submission tidak boleh ada setelah rollback: %v", err)
	}
	got, _ := r.photos.Get(ctx, p.ID)
	if got.Status != domain.PhotoPending || got.SubmissionID != nil {
		t.Fatalf("foto harus kembali pending: %+v", got)
	}
	var n int
	_ = r.pool.QueryRow(ctx, `SELECT count(*) FROM annotations WHERE photo_id=$1`, p.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("anotasi tidak boleh ada: %d", n)
	}
}

func TestTxRollbackOnPanicAndRepanics(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	func() {
		defer func() {
			if rec := recover(); rec != "meledak" {
				t.Fatalf("panic harus diteruskan, dapat %v", rec)
			}
		}()
		_ = r.tx.WithTx(ctx, func(ctx context.Context) error {
			_ = r.subs.Create(ctx, s)
			panic("meledak")
		})
	}()
	if _, err := r.subs.Get(ctx, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("panic harus me-rollback: %v", err)
	}
	// pool tetap sehat setelah panic
	if err := r.pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTxNestedJoinsOuter(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	boom := errors.New("luar gagal")
	err := r.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := r.tx.WithTx(ctx, func(ctx context.Context) error { return r.subs.Create(ctx, s) }); err != nil {
			return err
		}
		return boom // transaksi luar gagal → yang di dalam ikut dibatalkan
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := r.subs.Get(ctx, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("transaksi bersarang harus ikut rollback: %v", err)
	}
}

func TestTxIsolationFromOutside(t *testing.T) {
	r := newRig(t)
	s := newSub(nil)
	inside := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.tx.WithTx(ctx, func(ctx context.Context) error {
			if err := r.subs.Create(ctx, s); err != nil {
				return err
			}
			close(inside)
			<-release
			return nil
		})
	}()
	<-inside
	if _, err := r.subs.Get(ctx, s.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("data transaksi yang belum commit tidak boleh terlihat dari luar: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := r.subs.Get(ctx, s.ID); err != nil {
		t.Fatalf("setelah commit harus terlihat: %v", err)
	}
}

func TestTxBeginFailsOnCanceledContext(t *testing.T) {
	r := newRig(t)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	called := false
	err := r.tx.WithTx(cctx, func(ctx context.Context) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("konteks batal: err=%v called=%v", err, called)
	}
}

// Duplikat client_request_id di dalam transaksi membuat transaksi gagal; pemanggil harus
// rollback lalu membaca ulang di luar transaksi (pola idempotensi di usecase T14).
func TestTxDuplicateClientRequestThenReadOutside(t *testing.T) {
	r := newRig(t)
	first := newSub(nil)
	if err := r.subs.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	dup := newSub(nil)
	dup.ClientRequestID = first.ClientRequestID
	err := r.tx.WithTx(ctx, func(ctx context.Context) error { return r.subs.Create(ctx, dup) })
	if codeOf(err) != "DUPLICATE_CLIENT_REQUEST" {
		t.Fatalf("err = %v", err)
	}
	got, err := r.subs.GetByClientRequestID(ctx, first.ClientRequestID)
	if err != nil || got.ID != first.ID {
		t.Fatalf("pembacaan ulang di luar transaksi: %+v %v", got, err)
	}
}

// ---------------------------------------------------------------- MarkDeleted / SetPosition

func TestMarkDeletedAndSetPosition(t *testing.T) {
	r := newRig(t)
	s, other := newSub(nil), newSub(nil)
	_ = r.subs.Create(ctx, s)
	_ = r.subs.Create(ctx, other)
	a, b := r.uploadedPhoto(t, nil), r.uploadedPhoto(t, nil)
	_ = r.photos.Attach(ctx, a.ID, s.ID, 0, nil)
	_ = r.photos.Attach(ctx, b.ID, s.ID, 1, nil)

	if err := r.photos.SetPosition(ctx, a.ID, s.ID, 7); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.photos.Get(ctx, a.ID); got.Position != 7 || got.Status != domain.PhotoAttached {
		t.Fatalf("%+v", got)
	}
	// submission yang salah → konflik, tidak mengubah apa pun
	if err := r.photos.SetPosition(ctx, a.ID, other.ID, 1); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("SetPosition lintas submission: %v", err)
	}
	if err := r.photos.MarkDeleted(ctx, a.ID, other.ID); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("MarkDeleted lintas submission: %v", err)
	}
	if got, _ := r.photos.Get(ctx, a.ID); got.Status != domain.PhotoAttached || got.Position != 7 {
		t.Fatalf("tidak boleh berubah: %+v", got)
	}

	if err := r.photos.MarkDeleted(ctx, a.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.photos.Get(ctx, a.ID); got.Status != domain.PhotoDeleted {
		t.Fatalf("status = %s", got.Status)
	}
	// sudah deleted: tidak bisa dihapus/dipindah lagi
	if err := r.photos.MarkDeleted(ctx, a.ID, s.ID); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("MarkDeleted dua kali: %v", err)
	}
	if err := r.photos.SetPosition(ctx, a.ID, s.ID, 0); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("SetPosition pada foto deleted: %v", err)
	}
	// foto pending (tak punya submission) tidak bisa
	p := r.uploadedPhoto(t, nil)
	if err := r.photos.MarkDeleted(ctx, p.ID, s.ID); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("MarkDeleted pada pending: %v", err)
	}
	if err := r.photos.MarkDeleted(ctx, uuid.New(), s.ID); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("foto tak ada: %v", err)
	}
}
