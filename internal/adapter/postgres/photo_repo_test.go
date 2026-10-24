package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
)

func validPhoto() *domain.Photo {
	id := uuid.New()
	key, _ := domain.NewStorageKey(id, time.Now(), domain.ContentTypeWebP)
	return &domain.Photo{
		ID: id, StorageKey: key, ContentType: domain.ContentTypeWebP,
		SizeBytes: 412345, Width: 1920, Height: 1440,
		Status: domain.PhotoPending, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestPhotoRepoRoundTrip(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := postgres.NewPhotoRepo(pool)
	ctx := context.Background()

	// foto baru (pending): kolom opsional NULL
	p := validPhoto()
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID || got.StorageKey != p.StorageKey || got.ContentType != p.ContentType ||
		got.SizeBytes != p.SizeBytes || got.Width != p.Width || got.Height != p.Height ||
		got.Status != domain.PhotoPending || !got.CreatedAt.Equal(p.CreatedAt) || got.Position != 0 {
		t.Fatalf("round trip salah:\n got %+v\nwant %+v", got, p)
	}
	if got.SubmissionID != nil || got.OwnerID != nil || got.UploadedAt != nil || got.SHA256 != nil {
		t.Fatalf("kolom opsional harus NULL: %+v", got)
	}

	// foto lengkap: semua kolom terisi
	full := validPhoto()
	owner := uuid.New()
	subRow := newSub(nil)
	if err := postgres.NewSubmissionRepo(pool).Create(ctx, subRow); err != nil {
		t.Fatal(err)
	}
	sub := subRow.ID
	up := time.Now().UTC().Truncate(time.Microsecond)
	sum := bytes.Repeat([]byte{0xAB}, 32)
	full.OwnerID, full.SubmissionID, full.UploadedAt, full.SHA256 = &owner, &sub, &up, sum
	full.Status, full.Position = domain.PhotoAttached, 3
	if err := repo.Create(ctx, full); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(ctx, full.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerID == nil || *got.OwnerID != owner || got.SubmissionID == nil || *got.SubmissionID != sub ||
		got.UploadedAt == nil || !got.UploadedAt.Equal(up) || !bytes.Equal(got.SHA256, sum) ||
		got.Status != domain.PhotoAttached || got.Position != 3 {
		t.Fatalf("foto lengkap salah: %+v", got)
	}
}

func TestPhotoRepoGetUnknownIsNotFound(t *testing.T) {
	repo := postgres.NewPhotoRepo(pgtest.Pool(t))
	_, err := repo.Get(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPhotoRepoStorageKeyIsUnique(t *testing.T) {
	repo := postgres.NewPhotoRepo(pgtest.Pool(t))
	a := validPhoto()
	b := validPhoto()
	b.StorageKey = a.StorageKey
	if err := repo.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	err := repo.Create(context.Background(), b)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("storage_key kembar harus ditolak (unique_violation), dapat %v", err)
	}
}

// Constraint di DB adalah pagar kedua: tetap menolak walau aplikasi (bug) meloloskan nilai buruk.
func TestPhotoTableConstraints(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := postgres.NewPhotoRepo(pool)
	now := time.Now().UTC()
	sha := bytes.Repeat([]byte{1}, 32)
	subRow := newSub(nil)
	if err := postgres.NewSubmissionRepo(pool).Create(context.Background(), subRow); err != nil {
		t.Fatal(err)
	}
	sub := subRow.ID
	cases := map[string]func(*domain.Photo){
		"content-type tak diizinkan": func(p *domain.Photo) { p.ContentType = "image/png" },
		"ukuran 0":                   func(p *domain.Photo) { p.SizeBytes = 0 },
		"lebar 0":                    func(p *domain.Photo) { p.Width = 0 },
		"tinggi 0":                   func(p *domain.Photo) { p.Height = 0 },
		"lebar 2049":                 func(p *domain.Photo) { p.Width = 2049 },
		"status tak dikenal":         func(p *domain.Photo) { p.Status = "archived" },
		"sha256 salah panjang":       func(p *domain.Photo) { p.UploadedAt, p.SHA256 = &now, []byte{1, 2, 3} },
		"uploaded_at tanpa sha256":   func(p *domain.Photo) { p.UploadedAt = &now },
		"sha256 tanpa uploaded_at":   func(p *domain.Photo) { p.SHA256 = sha },
		"attached tanpa submission":  func(p *domain.Photo) { p.Status = domain.PhotoAttached },
		"position negatif":           func(p *domain.Photo) { p.Position = -1 },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			p := validPhoto()
			mut(p)
			err := repo.Create(context.Background(), p)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("harus ditolak check_violation (23514), dapat: %v", err)
			}
		})
	}
	// kontrol: kombinasi sah tidak ditolak
	ok := validPhoto()
	ok.UploadedAt, ok.SHA256, ok.Status, ok.SubmissionID = &now, sha, domain.PhotoAttached, &sub
	if err := repo.Create(context.Background(), ok); err != nil {
		t.Fatalf("kombinasi sah ditolak: %v", err)
	}
}
