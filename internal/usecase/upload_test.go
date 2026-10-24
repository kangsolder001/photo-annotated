package usecase_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/imagecheck"
	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/imgfix"
	"photo-annotated/internal/usecase"
)

var bg = context.Background()

type upRig struct {
	uc     *usecase.Upload
	repo   *memPhotoRepo
	dir    string
	clock  *fakeClock
	signer *signer.Signer
}

func newUpRig(t *testing.T, maxConc int) *upRig {
	t.Helper()
	dir := t.TempDir()
	store, err := localfs.New(localfs.Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return newUpRigWithStore(t, store, dir, maxConc)
}

func newUpRigWithStore(t *testing.T, store usecase.PhotoStore, dir string, maxConc int) *upRig {
	t.Helper()
	sg, err := signer.New(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	r := &upRig{
		repo:   newMemRepo(),
		dir:    dir,
		clock:  &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)},
		signer: sg,
	}
	r.uc = usecase.NewUpload(r.repo, store, sg, imagecheck.Inspector{}, r.clock, usecase.UploadConfig{MaxConcurrent: maxConc})
	return r
}

type tphoto struct {
	p   *domain.Photo
	sig string
	exp int64
}

// newPhoto membuat foto pending (seolah sudah lewat presign) dengan tanda tangan sah.
func (r *upRig) newPhoto(t *testing.T, ct string, size int64, w, h int) tphoto {
	t.Helper()
	id := uuid.New()
	key, err := domain.NewStorageKey(id, r.clock.t, ct)
	if err != nil {
		t.Fatal(err)
	}
	p := &domain.Photo{ID: id, StorageKey: key, ContentType: ct, SizeBytes: size, Width: w, Height: h,
		Status: domain.PhotoPending, CreatedAt: r.clock.t}
	if err := r.repo.Create(bg, p); err != nil {
		t.Fatal(err)
	}
	exp := r.clock.t.Add(10 * time.Minute)
	return tphoto{p: p, sig: r.signer.SignUpload(id, exp, ct, size), exp: exp.Unix()}
}

func (tp tphoto) input(body []byte) usecase.UploadInput {
	return usecase.UploadInput{
		PhotoID: tp.p.ID, Exp: tp.exp, Sig: tp.sig, ContentType: tp.p.ContentType,
		ContentLength: int64(len(body)), Body: bytes.NewReader(body),
	}
}

func codeOf(err error) string {
	if de, ok := domain.AsError(err); ok {
		return de.Code
	}
	return ""
}

func (r *upRig) finalPath(p *domain.Photo) string { return filepath.Join(r.dir, p.StorageKey) }

func (r *upRig) assertNothingPublished(t *testing.T, p *domain.Photo) {
	t.Helper()
	if _, err := os.Stat(r.finalPath(p)); !os.IsNotExist(err) {
		t.Fatalf("file tidak boleh terbit di lokasi akhir: %v", err)
	}
	ents, _ := os.ReadDir(filepath.Join(r.dir, "tmp"))
	if len(ents) != 0 {
		t.Fatalf("tmp harus bersih, ada %d entri", len(ents))
	}
}

func (r *upRig) status(t *testing.T, p *domain.Photo) *domain.Photo {
	t.Helper()
	got, err := r.repo.Get(bg, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// ---------------------------------------------------------------- jalur sukses

func TestUploadSuccessPublishesFileAndRecordsHash(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.WebP(1920, 1440, 400*1024, 0x5A)
	tp := r.newPhoto(t, "image/webp", int64(len(body)), 1920, 1440)

	if err := r.uc.Upload(bg, tp.input(body)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(r.finalPath(tp.p))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("isi file di SSD salah: %v", err)
	}
	p := r.status(t, tp.p)
	sum := sha256.Sum256(body)
	if p.UploadedAt == nil || !p.UploadedAt.Equal(r.clock.t) || !bytes.Equal(p.SHA256, sum[:]) {
		t.Fatalf("catatan DB salah: %+v", p)
	}
	if p.Status != domain.PhotoPending {
		t.Fatalf("status harus tetap pending sampai dipakai submission, dapat %s", p.Status)
	}
}

func TestUploadJPEG(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.JPEG(320, 240)
	tp := r.newPhoto(t, "image/jpeg", int64(len(body)), 320, 240)
	if err := r.uc.Upload(bg, tp.input(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.finalPath(tp.p)); err != nil {
		t.Fatal(err)
	}
}

// Sekali ter-upload, file tidak pernah berubah (regresi review: PUT ulang yang dimulai sebelum
// Attach bisa menimpa file foto yang sudah dipakai submission dan membuat sha256 tidak cocok).
func TestUploadIsImmutableOnceUploaded(t *testing.T) {
	r := newUpRig(t, 4)
	first := imgfix.WebP(100, 80, 2000, 0x01)
	second := imgfix.WebP(100, 80, 2000, 0x02) // ukuran sama, isi beda
	broken := bytes.Repeat([]byte{0}, 2000)    // bukan gambar
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	if err := r.uc.Upload(bg, tp.input(first)); err != nil {
		t.Fatal(err)
	}
	firstAt := r.status(t, tp.p).UploadedAt
	r.clock.t = r.clock.t.Add(time.Minute)

	// retry dengan isi sama: sukses tanpa mengubah apa pun
	if err := r.uc.Upload(bg, tp.input(first)); err != nil {
		t.Fatalf("retry isi sama harus sukses: %v", err)
	}
	// isi berbeda atau rusak: ditolak, file dan status tetap
	for _, body := range [][]byte{second, broken} {
		if err := r.uc.Upload(bg, tp.input(body)); codeOf(err) != "PHOTO_ALREADY_UPLOADED" {
			t.Fatalf("isi berbeda: %v", err)
		}
	}
	got, _ := os.ReadFile(r.finalPath(tp.p))
	p := r.status(t, tp.p)
	sum := sha256.Sum256(first)
	if !bytes.Equal(got, first) || !bytes.Equal(p.SHA256, sum[:]) || !p.UploadedAt.Equal(*firstAt) || p.Status != domain.PhotoPending {
		t.Fatalf("upload pertama harus utuh: %+v", p)
	}
	if ents, _ := os.ReadDir(filepath.Join(r.dir, "tmp")); len(ents) != 0 {
		t.Fatalf("tmp harus bersih, ada %d entri", len(ents))
	}
}

// ---------------------------------------------------------------- otorisasi

func TestUploadSignatureChecks(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	t.Run("tanda tangan salah", func(t *testing.T) {
		in := tp.input(body)
		in.Sig = tamper(tp.sig)
		if err := r.uc.Upload(bg, in); codeOf(err) != "INVALID_SIGNATURE" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("exp dimajukan oleh penyerang", func(t *testing.T) {
		in := tp.input(body)
		in.Exp += 3600
		if err := r.uc.Upload(bg, in); codeOf(err) != "INVALID_SIGNATURE" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("id tak dikenal dijawab sama dengan tanda tangan salah", func(t *testing.T) {
		in := tp.input(body)
		in.PhotoID = uuid.New()
		err := r.uc.Upload(bg, in)
		de, ok := domain.AsError(err)
		if !ok || de.Code != "INVALID_SIGNATURE" || de.Kind != domain.KindForbidden {
			t.Fatalf("keberadaan foto tidak boleh terbuka: %v", err)
		}
	})
	t.Run("tanda tangan dari foto lain", func(t *testing.T) {
		other := r.newPhoto(t, "image/webp", 2000, 100, 80)
		in := tp.input(body)
		in.Sig = other.sig
		if err := r.uc.Upload(bg, in); codeOf(err) != "INVALID_SIGNATURE" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("tanda tangan kosong", func(t *testing.T) {
		in := tp.input(body)
		in.Sig = ""
		if err := r.uc.Upload(bg, in); codeOf(err) != "INVALID_SIGNATURE" {
			t.Fatalf("err = %v", err)
		}
	})
	r.assertNothingPublished(t, tp.p)
}

func TestUploadExpiry(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	r.clock.t = time.Unix(tp.exp, 0) // tepat pada exp: masih berlaku
	if err := r.uc.Upload(bg, tp.input(body)); err != nil {
		t.Fatalf("tepat pada exp harus lolos: %v", err)
	}
	r.clock.t = time.Unix(tp.exp+1, 0) // satu detik lewat
	err := r.uc.Upload(bg, tp.input(body))
	de, ok := domain.AsError(err)
	if !ok || de.Code != "URL_EXPIRED" || de.Kind != domain.KindGone {
		t.Fatalf("err = %v, want URL_EXPIRED/Gone", err)
	}
	// tanda tangan salah + kedaluwarsa → tetap INVALID_SIGNATURE (kedaluwarsa tak boleh jadi oracle)
	in := tp.input(body)
	in.Sig = tamper(tp.sig)
	if err := r.uc.Upload(bg, in); codeOf(err) != "INVALID_SIGNATURE" {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadRejectsNonPendingPhotos(t *testing.T) {
	for _, st := range []domain.PhotoStatus{domain.PhotoAttached, domain.PhotoRejected, domain.PhotoDeleted} {
		t.Run(string(st), func(t *testing.T) {
			r := newUpRig(t, 4)
			body := imgfix.WebP(100, 80, 2000, 0)
			tp := r.newPhoto(t, "image/webp", 2000, 100, 80)
			r.repo.photos[tp.p.ID].Status = st
			err := r.uc.Upload(bg, tp.input(body))
			de, ok := domain.AsError(err)
			if !ok || de.Code != "PHOTO_NOT_AVAILABLE" || de.Kind != domain.KindConflict {
				t.Fatalf("err = %v", err)
			}
			r.assertNothingPublished(t, tp.p)
		})
	}
}

// ---------------------------------------------------------------- header

func TestUploadHeaderChecks(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	for name, ct := range map[string]string{
		"jpeg":             "image/jpeg",
		"dengan parameter": "image/webp; charset=binary",
		"huruf besar":      "IMAGE/WEBP",
		"kosong":           "",
		"octet-stream":     "application/octet-stream",
	} {
		in := tp.input(body)
		in.ContentType = ct
		if err := r.uc.Upload(bg, in); codeOf(err) != "INVALID_CONTENT_TYPE" {
			t.Errorf("Content-Type %s (%q): err = %v", name, ct, err)
		}
	}

	in := tp.input(body)
	in.ContentLength = -1
	err := r.uc.Upload(bg, in)
	if de, ok := domain.AsError(err); !ok || de.Code != "LENGTH_REQUIRED" || de.Kind != domain.KindLengthRequired {
		t.Errorf("tanpa Content-Length: %v", err)
	}
	for _, cl := range []int64{0, 1999, 2001, 1 << 30} {
		in := tp.input(body)
		in.ContentLength = cl
		if err := r.uc.Upload(bg, in); codeOf(err) != "SIZE_MISMATCH" {
			t.Errorf("Content-Length %d: err = %v", cl, err)
		}
	}
	r.assertNothingPublished(t, tp.p)
	if p := r.status(t, tp.p); p.Status != domain.PhotoPending || p.UploadedAt != nil {
		t.Fatalf("kegagalan header tidak boleh mengubah foto: %+v", p)
	}
}

// ---------------------------------------------------------------- isi body

func TestUploadBodySizeMismatch(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	// body lebih panjang dari yang dideklarasikan (Content-Length jujur, aliran tidak)
	long := append(append([]byte{}, body...), 0xFF)
	in := tp.input(body)
	in.Body = bytes.NewReader(long)
	if err := r.uc.Upload(bg, in); codeOf(err) != "SIZE_MISMATCH" {
		t.Fatalf("body kelebihan: %v", err)
	}
	// body lebih pendek (koneksi putus di tengah → ErrUnexpectedEOF dari net/http)
	in = tp.input(body)
	in.Body = io.MultiReader(bytes.NewReader(body[:1000]), errReader{io.ErrUnexpectedEOF})
	if err := r.uc.Upload(bg, in); codeOf(err) != "SIZE_MISMATCH" {
		t.Fatalf("body kurang: %v", err)
	}
	// body habis lebih awal tanpa error (EOF bersih)
	in = tp.input(body)
	in.Body = bytes.NewReader(body[:1500])
	if err := r.uc.Upload(bg, in); codeOf(err) != "SIZE_MISMATCH" {
		t.Fatalf("EOF bersih lebih awal: %v", err)
	}
	r.assertNothingPublished(t, tp.p)
	if p := r.status(t, tp.p); p.Status != domain.PhotoPending || p.UploadedAt != nil {
		t.Fatalf("foto harus tetap pending dan belum ter-upload agar bisa diulang: %+v", p)
	}
	// setelah kegagalan, upload yang benar tetap bisa
	if err := r.uc.Upload(bg, tp.input(body)); err != nil {
		t.Fatalf("ulang setelah gagal: %v", err)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestUploadSlowClientTimeoutAndOtherReadErrors(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	in := tp.input(body)
	in.Body = io.MultiReader(bytes.NewReader(body[:100]), errReader{os.ErrDeadlineExceeded})
	err := r.uc.Upload(bg, in)
	if de, ok := domain.AsError(err); !ok || de.Code != "UPLOAD_TIMEOUT" || de.Kind != domain.KindTimeout {
		t.Fatalf("deadline baca terlewati: %v", err)
	}
	boom := errors.New("connection reset by peer")
	in = tp.input(body)
	in.Body = io.MultiReader(bytes.NewReader(body[:100]), errReader{boom})
	if err := r.uc.Upload(bg, in); !errors.Is(err, boom) {
		t.Fatalf("error baca lain harus diteruskan (dicatat di log): %v", err)
	}
	r.assertNothingPublished(t, tp.p)
}

func TestUploadInvalidImagesAreRejectedAndNeverPublished(t *testing.T) {
	webpBody := imgfix.WebP(100, 80, 2000, 0)
	jpegBody := imgfix.JPEG(64, 48)
	tests := []struct {
		name string
		ct   string
		body []byte
		w, h int
	}{
		{"byte acak", "image/webp", imgfix.Junk(2000), 100, 80},
		{"jpeg dikirim sebagai webp", "image/webp", jpegBody, 64, 48},
		{"webp dikirim sebagai jpeg", "image/jpeg", webpBody, 100, 80},
		{"webp dimensi tidak cocok", "image/webp", imgfix.WebP(101, 80, 2000, 0), 100, 80},
		{"jpeg dimensi tidak cocok", "image/jpeg", jpegBody, 64, 49},
		{"html menyamar", "image/jpeg", append([]byte("<script>alert(1)</script>"), make([]byte, 100)...), 10, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newUpRig(t, 4)
			tp := r.newPhoto(t, tc.ct, int64(len(tc.body)), tc.w, tc.h)
			err := r.uc.Upload(bg, tp.input(tc.body))
			de, ok := domain.AsError(err)
			if !ok || de.Code != "INVALID_IMAGE" {
				t.Fatalf("err = %v, want INVALID_IMAGE", err)
			}
			r.assertNothingPublished(t, tp.p)
			if p := r.status(t, tp.p); p.Status != domain.PhotoRejected || p.UploadedAt != nil {
				t.Fatalf("foto harus rejected dan tidak ter-upload: %+v", p)
			}
			// setelah rejected, upload lagi ditolak (klien harus presign ulang)
			if err := r.uc.Upload(bg, tp.input(tc.body)); codeOf(err) != "PHOTO_NOT_AVAILABLE" {
				t.Fatalf("setelah rejected: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------- penyimpanan

func TestUploadStorageNotReady(t *testing.T) {
	st := &fakeStore{readyErr: domain.Unavailable("STORAGE_UNAVAILABLE", "photo storage is unavailable")}
	r := newUpRigWithStore(t, st, t.TempDir(), 1)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)
	err := r.uc.Upload(bg, tp.input(body))
	if de, ok := domain.AsError(err); !ok || de.Code != "STORAGE_UNAVAILABLE" || de.Kind != domain.KindUnavailable {
		t.Fatalf("err = %v", err)
	}
	// slot harus dilepas: setelah pulih, upload berikutnya tidak kena UPLOAD_BUSY
	st.readyErr = domain.Unavailable("STORAGE_FULL", "photo storage is full")
	if err := r.uc.Upload(bg, tp.input(body)); codeOf(err) != "STORAGE_FULL" {
		t.Fatalf("slot bocor atau error salah: %v", err)
	}
}

func TestUploadMarkUploadedRaceLeavesOnlyOrphanFile(t *testing.T) {
	r := newUpRig(t, 4)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)
	// foto dihapus (mis. edit/cleanup) tepat setelah file terbit dan sebelum dicatat
	r.repo.beforeMark = func(p *domain.Photo) { p.Status = domain.PhotoDeleted }
	err := r.uc.Upload(bg, tp.input(body))
	if de, ok := domain.AsError(err); !ok || de.Code != "PHOTO_NOT_AVAILABLE" || de.Kind != domain.KindConflict {
		t.Fatalf("err = %v", err)
	}
	// yang tersisa hanya file yatim (akan disapu cleanup), bukan baris DB tanpa file
	if _, err := os.Stat(r.finalPath(tp.p)); err != nil {
		t.Fatalf("file yatim diharapkan ada untuk disapu cleanup: %v", err)
	}
	if p := r.status(t, tp.p); p.UploadedAt != nil || p.SHA256 != nil {
		t.Fatalf("DB tidak boleh mencatat upload: %+v", p)
	}
}

// ---------------------------------------------------------------- konkurensi

// pipeUpload memulai upload yang sengaja menggantung di tengah aliran, dan baru kembali
// setelah usecase benar-benar membaca byte pertama (artinya slot dan kunci sudah dipegang).
type pipeUpload struct {
	pw   *io.PipeWriter
	body []byte
	done chan error
}

func startPipeUpload(t *testing.T, r *upRig, tp tphoto, body []byte, ctx context.Context) *pipeUpload {
	t.Helper()
	pr, pw := io.Pipe()
	in := tp.input(body)
	in.Body = pr
	pu := &pipeUpload{pw: pw, body: body, done: make(chan error, 1)}
	go func() { pu.done <- r.uc.Upload(ctx, in) }()
	if _, err := pw.Write(body[:1]); err != nil { // blokir sampai dibaca Stage
		t.Fatal(err)
	}
	return pu
}

func (p *pipeUpload) finish(t *testing.T) error {
	t.Helper()
	if _, err := p.pw.Write(p.body[1:]); err != nil {
		t.Fatal(err)
	}
	_ = p.pw.Close()
	return <-p.done
}

func TestUploadSlotsAreLimitedAndReleased(t *testing.T) {
	r := newUpRig(t, 2)
	body := imgfix.WebP(100, 80, 2000, 0)
	a, b, c, d := r.newPhoto(t, "image/webp", 2000, 100, 80), r.newPhoto(t, "image/webp", 2000, 100, 80),
		r.newPhoto(t, "image/webp", 2000, 100, 80), r.newPhoto(t, "image/webp", 2000, 100, 80)

	ua := startPipeUpload(t, r, a, body, bg)
	ub := startPipeUpload(t, r, b, body, bg)

	err := r.uc.Upload(bg, c.input(body)) // slot ke-3
	de, ok := domain.AsError(err)
	if !ok || de.Code != "UPLOAD_BUSY" || de.Kind != domain.KindUnavailable || de.RetryAfter <= 0 {
		t.Fatalf("slot penuh harus UPLOAD_BUSY dengan Retry-After: %v", err)
	}
	if err := ua.finish(t); err != nil {
		t.Fatal(err)
	}
	if err := r.uc.Upload(bg, c.input(body)); err != nil { // slot a dilepas
		t.Fatalf("slot harus tersedia setelah satu upload selesai: %v", err)
	}
	if err := ub.finish(t); err != nil {
		t.Fatal(err)
	}
	// kegagalan juga melepas slot
	if err := r.uc.Upload(bg, d.input(imgfix.Junk(2000))); codeOf(err) != "INVALID_IMAGE" {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		e := r.newPhoto(t, "image/webp", 2000, 100, 80)
		if err := r.uc.Upload(bg, e.input(body)); err != nil {
			t.Fatalf("slot bocor setelah kegagalan: %v", err)
		}
	}
}

func TestUploadSamePhotoConcurrentIsRejected(t *testing.T) {
	r := newUpRig(t, 8)
	body := imgfix.WebP(100, 80, 2000, 0x07)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	first := startPipeUpload(t, r, tp, body, bg)
	err := r.uc.Upload(bg, tp.input(body))
	de, ok := domain.AsError(err)
	if !ok || de.Code != "UPLOAD_IN_PROGRESS" || de.Kind != domain.KindConflict || de.RetryAfter <= 0 {
		t.Fatalf("upload kedua pada foto yang sama harus UPLOAD_IN_PROGRESS: %v", err)
	}
	if err := first.finish(t); err != nil {
		t.Fatal(err)
	}
	if err := r.uc.Upload(bg, tp.input(body)); err != nil {
		t.Fatalf("setelah yang pertama selesai, PUT ulang harus bisa: %v", err)
	}
	// hash di DB cocok dengan file di disk
	got, _ := os.ReadFile(r.finalPath(tp.p))
	sum := sha256.Sum256(got)
	if p := r.status(t, tp.p); !bytes.Equal(p.SHA256, sum[:]) {
		t.Fatal("hash DB tidak cocok dengan file di disk")
	}
}

func TestUploadClientDisconnectCleansUpAndFreesSlot(t *testing.T) {
	r := newUpRig(t, 1)
	body := imgfix.WebP(100, 80, 2000, 0)
	tp := r.newPhoto(t, "image/webp", 2000, 100, 80)

	ctx, cancel := context.WithCancel(bg)
	pr, pw := io.Pipe()
	in := tp.input(body)
	in.Body = pr
	done := make(chan error, 1)
	go func() { done <- r.uc.Upload(ctx, in) }()
	if _, err := pw.Write(body[:10]); err != nil {
		t.Fatal(err)
	}
	cancel() // klien putus
	go func() { _, _ = pw.Write(body[10:20]) }()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	pw.Close()
	r.assertNothingPublished(t, tp.p)
	// satu-satunya slot harus sudah bebas
	if err := r.uc.Upload(bg, tp.input(body)); err != nil {
		t.Fatalf("slot harus bebas setelah klien putus: %v", err)
	}
}

func TestUploadManyConcurrentDistinctPhotos(t *testing.T) {
	r := newUpRig(t, 64)
	const n = 64
	var wg sync.WaitGroup
	photos := make([]tphoto, n)
	bodies := make([][]byte, n)
	for i := range photos {
		bodies[i] = imgfix.WebP(100, 80, 20_000+2*i, byte(i))
		photos[i] = r.newPhoto(t, "image/webp", int64(len(bodies[i])), 100, 80)
	}
	for i := range photos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.uc.Upload(bg, photos[i].input(bodies[i])); err != nil {
				t.Errorf("upload %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	for i := range photos {
		got, err := os.ReadFile(r.finalPath(photos[i].p))
		if err != nil || !bytes.Equal(got, bodies[i]) {
			t.Errorf("file %d salah: %v", i, err)
		}
		sum := sha256.Sum256(bodies[i])
		if p := r.status(t, photos[i].p); !bytes.Equal(p.SHA256, sum[:]) {
			t.Errorf("hash %d salah", i)
		}
	}
}

func TestNewUploadPanicsOnInvalidConfig(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MaxConcurrent 0 harus panic")
		}
	}()
	usecase.NewUpload(newMemRepo(), &fakeStore{}, nil, imagecheck.Inspector{}, usecase.SystemClock{}, usecase.UploadConfig{})
}

// tamper mengubah karakter pertama sehingga hasilnya pasti berbeda dari sig asli.
func tamper(sig string) string {
	if sig[0] == 'A' {
		return "B" + sig[1:]
	}
	return "A" + sig[1:]
}
