// Package usecase berisi logika aplikasi. Paket ini hanya mengenal domain dan interface
// (port) yang didefinisikan di sini; adapter di luar mengimplementasikannya.
package usecase

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// ErrFileNotFound dikembalikan PhotoStore.Open/Stat jika file tidak ada.
var ErrFileNotFound = errors.New("file not found")

// FileInfo adalah metadata file di penyimpanan foto.
type FileInfo struct {
	Size    int64
	ModTime time.Time
}

// File adalah file yang bisa dibaca dan di-seek (dipakai http.ServeContent untuk Range).
type File interface {
	io.ReadSeeker
	io.Closer
	Info() FileInfo
}

// StagedFile adalah upload yang sudah tertulis penuh di area sementara tetapi belum
// "terbit" di lokasi akhir. Pemanggil wajib memanggil Commit atau Discard (Discard aman
// dipanggil setelah Commit, sehingga cocok untuk defer).
type StagedFile interface {
	// Size dan SHA256 dihitung dari byte yang benar-benar tertulis.
	Size() int64
	SHA256() []byte
	// Open membaca isi dari awal, untuk validasi sebelum Commit.
	Open() (io.ReadCloser, error)
	// Commit membuat file terbit secara atomik di lokasi akhir (menimpa yang lama).
	Commit() error
	// Discard membuang file sementara. Idempotent.
	Discard() error
}

// PhotoStore adalah penyimpanan byte foto. v1 hanya punya adapter filesystem lokal (SSD).
type PhotoStore interface {
	// Ready memeriksa penyimpanan siap menerima tulisan (ada, bisa ditulis, sisa ruang cukup).
	// Hasil boleh di-cache singkat. Error berupa *domain.Error: STORAGE_UNAVAILABLE/STORAGE_FULL.
	Ready(ctx context.Context) error
	// Stage men-stream r ke area sementara dengan buffer tetap (memori tidak bergantung ukuran
	// file) sambil menghitung SHA-256. Melebihi maxBytes → error SIZE_MISMATCH.
	Stage(ctx context.Context, key string, r io.Reader, maxBytes int64) (StagedFile, error)
	Open(ctx context.Context, key string) (File, error)
	Stat(ctx context.Context, key string) (FileInfo, error)
	// Delete idempotent: file yang tidak ada dianggap sukses.
	Delete(ctx context.Context, key string) error
}

// Clock memungkinkan waktu dikendalikan di test.
type Clock interface{ Now() time.Time }

// SystemClock adalah Clock produksi.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// PhotoRepo menyimpan metadata foto.
type PhotoRepo interface {
	// Create menyimpan foto baru (status pending). storage_key harus unik.
	Create(ctx context.Context, p *domain.Photo) error
	// Get mengembalikan foto, atau error yang cocok dengan domain.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (*domain.Photo, error)
	// MarkUploaded mencatat upload sukses (uploaded_at + sha256) pada foto yang masih pending.
	// Jika foto sudah tidak pending, mengembalikan domain.Conflict PHOTO_NOT_AVAILABLE.
	MarkUploaded(ctx context.Context, id uuid.UUID, at time.Time, sha256 []byte) error
	// MarkRejected menandai foto pending sebagai rejected. Foto yang sudah bukan pending diabaikan.
	MarkRejected(ctx context.Context, id uuid.UUID) error
	// Attach memakai foto untuk sebuah submission secara atomik: hanya berhasil bila foto masih
	// pending, sudah ter-upload, dan pemiliknya sama (ownerID, nil = anonim). Selain itu
	// mengembalikan domain.Conflict PHOTO_NOT_AVAILABLE; ini yang mencegah satu foto dipakai
	// dua submission walau dua request datang bersamaan.
	Attach(ctx context.Context, photoID, submissionID uuid.UUID, position int, ownerID *uuid.UUID) error
	// ListBySubmission mengembalikan foto attached milik submission, berurutan menurut position.
	ListBySubmission(ctx context.Context, submissionID uuid.UUID) ([]*domain.Photo, error)
}

// TxManager menjalankan fn dalam satu transaksi database. Repository yang dipanggil dengan
// ctx dari fn ikut dalam transaksi itu. fn error atau panic → rollback.
type TxManager interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// SubmissionRepo menyimpan submission dan anotasinya.
type SubmissionRepo interface {
	// Create menyimpan submission baru. client_request_id yang sudah ada →
	// domain.Conflict DUPLICATE_CLIENT_REQUEST.
	Create(ctx context.Context, s *domain.Submission) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Submission, error)
	GetByClientRequestID(ctx context.Context, id uuid.UUID) (*domain.Submission, error)
	// GetDetail mengembalikan submission + foto attached + anotasi sebagai satu snapshot konsisten.
	GetDetail(ctx context.Context, id uuid.UUID) (*domain.SubmissionDetail, error)
	// AddAnnotations menambahkan anotasi ke foto, mempertahankan urutan.
	AddAnnotations(ctx context.Context, photoID uuid.UUID, anns []domain.Annotation) error
	// ReplaceAnnotations mengganti seluruh anotasi sebuah foto (hapus lalu tambah).
	ReplaceAnnotations(ctx context.Context, photoID uuid.UUID, anns []domain.Annotation) error
	// BumpVersion menaikkan version dan updated_at bila version saat ini == expected.
	// Tidak cocok → domain.Conflict VERSION_CONFLICT; id tak ada → domain.ErrNotFound.
	BumpVersion(ctx context.Context, id uuid.UUID, expected int, now time.Time) (newVersion int, err error)
}

// ImageInfo adalah hasil pemeriksaan header gambar.
type ImageInfo struct{ Width, Height int }

// ImageInspector memeriksa isi file sesuai content-type yang dideklarasikan tanpa men-decode
// piksel (magic bytes + header). Ketidakcocokan → *domain.Error INVALID_IMAGE.
type ImageInspector interface {
	Inspect(r io.Reader, contentType string) (ImageInfo, error)
}

// URLSigner menandatangani dan memverifikasi URL upload/lihat foto. Setiap tujuan memakai
// awalan pesan sendiri sehingga tanda tangan satu tujuan tidak berlaku untuk yang lain.
type URLSigner interface {
	SignUpload(photoID uuid.UUID, exp time.Time, contentType string, sizeBytes int64) string
	VerifyUpload(photoID uuid.UUID, exp int64, contentType string, sizeBytes int64, sig string) bool
}
