package usecase_test

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time { return f.t }

type memPhotoRepo struct {
	mu         sync.Mutex
	photos     map[uuid.UUID]*domain.Photo
	createErr  error
	beforeMark func(*domain.Photo) // dipanggil di dalam MarkUploaded (untuk mensimulasikan balapan)
}

func newMemRepo() *memPhotoRepo { return &memPhotoRepo{photos: map[uuid.UUID]*domain.Photo{}} }

func (r *memPhotoRepo) Create(_ context.Context, p *domain.Photo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	cp := *p
	r.photos[p.ID] = &cp
	return nil
}

func (r *memPhotoRepo) Get(_ context.Context, id uuid.UUID) (*domain.Photo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.photos[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}

func (r *memPhotoRepo) MarkUploaded(_ context.Context, id uuid.UUID, at time.Time, sha []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beforeMark != nil {
		r.beforeMark(r.photos[id])
	}
	p, ok := r.photos[id]
	if !ok || p.Status != domain.PhotoPending {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo is no longer pending")
	}
	p.UploadedAt, p.SHA256 = &at, append([]byte(nil), sha...)
	return nil
}

func (r *memPhotoRepo) MarkRejected(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.photos[id]; ok && p.Status == domain.PhotoPending {
		p.Status = domain.PhotoRejected
	}
	return nil
}

func (r *memPhotoRepo) Attach(_ context.Context, photoID, submissionID uuid.UUID, position int, ownerID *uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.photos[photoID]
	sameOwner := ok && ((p.OwnerID == nil && ownerID == nil) || (p.OwnerID != nil && ownerID != nil && *p.OwnerID == *ownerID))
	if !ok || p.Status != domain.PhotoPending || p.UploadedAt == nil || !sameOwner {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo cannot be attached")
	}
	p.SubmissionID, p.Position, p.Status = &submissionID, position, domain.PhotoAttached
	return nil
}

func (r *memPhotoRepo) ListBySubmission(_ context.Context, submissionID uuid.UUID) ([]*domain.Photo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Photo
	for _, p := range r.photos {
		if p.SubmissionID != nil && *p.SubmissionID == submissionID && p.Status == domain.PhotoAttached {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *memPhotoRepo) MarkDeleted(_ context.Context, photoID, submissionID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.photos[photoID]
	if !ok || p.SubmissionID == nil || *p.SubmissionID != submissionID || p.Status != domain.PhotoAttached {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo is not attached to this submission")
	}
	p.Status = domain.PhotoDeleted
	return nil
}

func (r *memPhotoRepo) SetPosition(_ context.Context, photoID, submissionID uuid.UUID, position int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.photos[photoID]
	if !ok || p.SubmissionID == nil || *p.SubmissionID != submissionID || p.Status != domain.PhotoAttached {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo is not attached to this submission")
	}
	p.Position = position
	return nil
}

func (r *memPhotoRepo) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.photos) }

// fakeStore hanya mengimplementasikan Ready; metode lain akan panic bila terpanggil (nil embed).
type fakeStore struct {
	usecase.PhotoStore
	readyErr   error
	readyCalls int
}

func (f *fakeStore) Ready(context.Context) error { f.readyCalls++; return f.readyErr }

const testSecret = "0123456789abcdef0123456789abcdef0123456789abcdef"

type rig struct {
	uc     *usecase.Presign
	repo   *memPhotoRepo
	store  *fakeStore
	clock  *fakeClock
	signer *signer.Signer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	s, err := signer.New(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{
		repo:   newMemRepo(),
		store:  &fakeStore{},
		clock:  &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)},
		signer: s,
	}
	r.uc = usecase.NewPresign(r.repo, r.store, s, r.clock, usecase.PresignConfig{
		PublicBaseURL: "https://api.example.com", MaxUploadBytes: 2 << 20, UploadURLTTL: 10 * time.Minute,
	})
	return r
}

func okInput() usecase.PresignInput {
	return usecase.PresignInput{ContentType: "image/webp", SizeBytes: 412345, Width: 1920, Height: 1440}
}

func TestPresignSuccess(t *testing.T) {
	r := newRig(t)
	owner := uuid.New()
	in := okInput()
	in.OwnerID = &owner
	out, err := r.uc.Presign(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	if out.Method != "PUT" || out.Headers["Content-Type"] != "image/webp" {
		t.Fatalf("method/header salah: %+v", out)
	}
	if want := r.clock.t.Add(10 * time.Minute); !out.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", out.ExpiresAt, want)
	}
	if out.PhotoID.Version() != 7 {
		t.Fatalf("id harus UUIDv7, versi %d", out.PhotoID.Version())
	}

	u, err := url.Parse(out.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host != "https://api.example.com" || u.Path != "/api/v1/uploads/"+out.PhotoID.String() {
		t.Fatalf("URL salah: %s", out.URL)
	}
	exp, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if exp != out.ExpiresAt.Unix() {
		t.Fatalf("exp di URL = %d", exp)
	}
	sig := u.Query().Get("sig")
	if !r.signer.VerifyUpload(out.PhotoID, exp, "image/webp", 412345, sig) {
		t.Fatal("tanda tangan di URL harus sah untuk (id, exp, content-type, ukuran) ini")
	}
	if r.signer.VerifyUpload(out.PhotoID, exp, "image/webp", 412346, sig) {
		t.Fatal("tanda tangan harus terikat pada ukuran")
	}

	p, err := r.repo.Get(context.Background(), out.PhotoID)
	if err != nil {
		t.Fatal(err)
	}
	wantKey := "2026/10/24/" + out.PhotoID.String() + ".webp"
	if p.StorageKey != wantKey || !domain.IsValidStorageKey(p.StorageKey) {
		t.Fatalf("storage key = %q, want %q", p.StorageKey, wantKey)
	}
	if p.Status != domain.PhotoPending || p.SizeBytes != 412345 || p.Width != 1920 || p.Height != 1440 ||
		p.ContentType != "image/webp" || !p.CreatedAt.Equal(r.clock.t) {
		t.Fatalf("foto tersimpan salah: %+v", p)
	}
	if p.OwnerID == nil || *p.OwnerID != owner {
		t.Fatalf("owner = %v", p.OwnerID)
	}
	if p.UploadedAt != nil || p.SHA256 != nil || p.SubmissionID != nil {
		t.Fatalf("foto baru tidak boleh punya uploaded_at/sha/submission: %+v", p)
	}
}

func TestPresignJPEGAndAnonymousOwner(t *testing.T) {
	r := newRig(t)
	in := okInput()
	in.ContentType = "image/jpeg"
	out, err := r.uc.Presign(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := r.repo.Get(context.Background(), out.PhotoID)
	if !strings.HasSuffix(p.StorageKey, ".jpg") || p.OwnerID != nil {
		t.Fatalf("jpeg/anonim salah: %+v", p)
	}
}

func TestPresignValidationErrorsBeforeAnySideEffect(t *testing.T) {
	cases := map[string]struct {
		mut  func(*usecase.PresignInput)
		code string
	}{
		"content-type": {func(i *usecase.PresignInput) { i.ContentType = "image/png" }, "INVALID_CONTENT_TYPE"},
		"ukuran 0":     {func(i *usecase.PresignInput) { i.SizeBytes = 0 }, "INVALID_SIZE"},
		"ukuran besar": {func(i *usecase.PresignInput) { i.SizeBytes = 2<<20 + 1 }, "INVALID_SIZE"},
		"dimensi":      {func(i *usecase.PresignInput) { i.Width = 2049 }, "INVALID_DIMENSIONS"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.store.readyErr = errors.New("tidak boleh ditanyakan") // validasi harus mendahului
			in := okInput()
			tc.mut(&in)
			_, err := r.uc.Presign(context.Background(), in)
			de, ok := domain.AsError(err)
			if !ok || de.Code != tc.code || de.Kind != domain.KindInvalid {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
			if r.repo.count() != 0 || r.store.readyCalls != 0 {
				t.Fatalf("tidak boleh ada efek samping: foto=%d ready=%d", r.repo.count(), r.store.readyCalls)
			}
		})
	}
}

func TestPresignRefusesWhenStorageNotReady(t *testing.T) {
	r := newRig(t)
	r.store.readyErr = domain.Unavailable("STORAGE_FULL", "photo storage is full")
	_, err := r.uc.Presign(context.Background(), okInput())
	de, ok := domain.AsError(err)
	if !ok || de.Code != "STORAGE_FULL" || de.Kind != domain.KindUnavailable {
		t.Fatalf("err = %v", err)
	}
	if r.repo.count() != 0 {
		t.Fatal("baris foto tidak boleh dibuat bila penyimpanan tidak siap")
	}
}

func TestPresignRepoFailureReturnsNoURL(t *testing.T) {
	r := newRig(t)
	boom := errors.New("db mati")
	r.repo.createErr = boom
	out, err := r.uc.Presign(context.Background(), okInput())
	if !errors.Is(err, boom) || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestPresignIDsAndKeysAreUnique(t *testing.T) {
	r := newRig(t)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		out, err := r.uc.Presign(context.Background(), okInput())
		if err != nil {
			t.Fatal(err)
		}
		p, _ := r.repo.Get(context.Background(), out.PhotoID)
		for _, v := range []string{out.PhotoID.String(), p.StorageKey} {
			if seen[v] {
				t.Fatalf("duplikat: %s", v)
			}
			seen[v] = true
		}
	}
}
