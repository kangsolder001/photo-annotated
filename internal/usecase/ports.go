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

// ErrInvalidStorageKey dikembalikan PhotoStore bila kunci tidak berbentuk kanonik. Kunci seperti
// itu tidak pernah menunjuk file, jadi tidak ada yang perlu dihapus.
var ErrInvalidStorageKey = errors.New("invalid storage key")

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
	// SweepTmp menghapus sisa upload sementara (tmp/*.part dan berkas probe) yang lebih tua dari
	// cutoff; mengembalikan jumlah yang dihapus.
	SweepTmp(ctx context.Context, cutoff time.Time) (int, error)
	// WalkOlderThan memanggil fn untuk tiap file foto (kunci kanonik) yang dimodifikasi sebelum
	// cutoff, berurutan menurut kunci. tmp/ dan berkas non-foto dilewati.
	WalkOlderThan(ctx context.Context, cutoff time.Time, fn func(key string) error) error
}

// PhotoRef menunjuk satu baris foto beserta lokasi file-nya.
type PhotoRef struct {
	ID         uuid.UUID
	StorageKey string
}

// CleanupRepo berisi kueri untuk job pembersihan dan retensi (spec §8.3). Semuanya aman
// dijalankan berulang dan oleh lebih dari satu instance (FOR UPDATE SKIP LOCKED).
type CleanupRepo interface {
	// ClaimStalePending mengubah foto pending yang dibuat sebelum cutoff menjadi deleted secara
	// atomik. Setelah di-claim, foto tidak bisa lagi dilampirkan (Attach hanya menerima pending).
	ClaimStalePending(ctx context.Context, cutoff time.Time, limit int) (int, error)
	// ListDeletablePhotos mengembalikan foto rejected/deleted dengan id > afterID, berurutan id.
	ListDeletablePhotos(ctx context.Context, afterID uuid.UUID, limit int) ([]PhotoRef, error)
	// DeletePhotoRow menghapus baris foto rejected/deleted (anotasinya ikut lewat CASCADE).
	DeletePhotoRow(ctx context.Context, id uuid.UUID) error
	// ExpireSubmissions menandai semua foto milik submission yang dibuat sebelum cutoff sebagai
	// deleted; mengembalikan jumlah foto yang ditandai. Submission yang fotonya sudah deleted dilewati.
	ExpireSubmissions(ctx context.Context, cutoff time.Time, limit int) (int, error)
	// DeleteEmptyExpiredSubmissions menghapus submission kedaluwarsa yang sudah tidak punya baris foto.
	DeleteEmptyExpiredSubmissions(ctx context.Context, cutoff time.Time, limit int) (int, error)
	// ExistingStorageKeys mengembalikan himpunan kunci yang punya baris foto (status apa pun).
	ExistingStorageKeys(ctx context.Context, keys []string) (map[string]bool, error)
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
	// MarkDeleted menandai foto attached milik submission sebagai deleted (file dihapus cleanup).
	// Bukan lagi attached pada submission itu → domain.Conflict PHOTO_NOT_AVAILABLE.
	MarkDeleted(ctx context.Context, photoID, submissionID uuid.UUID) error
	// SetPosition mengubah urutan tampil foto attached pada submission.
	SetPosition(ctx context.Context, photoID, submissionID uuid.UUID, position int) error
}

// UserRepo menyimpan pengguna (auth.mode = otp).
type UserRepo interface {
	// FindOrCreatePending membuat pengguna pending baru, atau mengembalikan yang sudah ada untuk
	// nomor yang sama TANPA menimpa nama/email-nya (created=false). Aman terhadap balapan.
	FindOrCreatePending(ctx context.Context, u *domain.User) (user *domain.User, created bool, err error)
	Get(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByPhone(ctx context.Context, phone string) (*domain.User, error)
	// Activate menandai nomor terverifikasi dan mencatat login (idempoten untuk pengguna aktif).
	Activate(ctx context.Context, id uuid.UUID, now time.Time) error
	// DeletePendingBefore menghapus pengguna pending yang dibuat sebelum cutoff.
	DeletePendingBefore(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

// OTPRepo menyimpan challenge OTP dan menjadi antrean pengiriman.
type OTPRepo interface {
	// Replace menandai challenge aktif milik nomor itu sebagai expired lalu menyimpan c yang baru,
	// dalam satu transaksi (hanya satu challenge aktif per nomor).
	Replace(ctx context.Context, c *domain.OTPChallenge) error
	Get(ctx context.Context, id uuid.UUID) (*domain.OTPChallenge, error)
	// LatestForPhone mengembalikan challenge terbaru milik nomor, atau domain.ErrNotFound.
	LatestForPhone(ctx context.Context, phone string) (*domain.OTPChallenge, error)
	CountForPhoneSince(ctx context.Context, phone string, since time.Time) (int, error)
	CountCreatedSince(ctx context.Context, since time.Time) (int, error)
	CountQueued(ctx context.Context) (int, error)

	// ClaimNext mengambil challenge queued yang siap (next_attempt_at <= now), mengubahnya menjadi
	// sending (next_attempt_at diisi now sebagai penanda waktu klaim) dan menaikkan send_attempts.
	// domain.ErrNotFound bila antrean kosong.
	ClaimNext(ctx context.Context, now time.Time) (*domain.OTPChallenge, error)
	// RequeueStuck mengembalikan challenge sending yang diklaim sebelum olderThan (worker mati
	// di tengah kirim) ke queued, siap dikirim sejak now.
	RequeueStuck(ctx context.Context, olderThan, now time.Time) (int, error)
	MarkSent(ctx context.Context, id uuid.UUID, waMessageID string, sentAt, expiresAt time.Time) error
	Requeue(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, reason string) error

	// IncrementVerifyAttempts menaikkan percobaan verifikasi dan mengembalikan nilai barunya.
	IncrementVerifyAttempts(ctx context.Context, id uuid.UUID) (int, error)
	// Consume menandai challenge sent → consumed sekali pakai; false bila sudah bukan sent.
	Consume(ctx context.Context, id uuid.UUID) (bool, error)
	// Expire menandai challenge aktif sebagai expired.
	Expire(ctx context.Context, id uuid.UUID) error
	DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

// SessionRepo menyimpan sesi login.
type SessionRepo interface {
	Create(ctx context.Context, s *domain.Session) error
	// GetByTokenHash mengembalikan sesi, atau domain.ErrNotFound.
	GetByTokenHash(ctx context.Context, hash []byte) (*domain.Session, error)
	Revoke(ctx context.Context, id uuid.UUID, now time.Time) error
	// DeleteInactiveBefore menghapus sesi yang kedaluwarsa atau dicabut sebelum cutoff.
	DeleteInactiveBefore(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

// EditTokenSigner menurunkan dan memverifikasi token edit anonim (hanya auth.mode = none).
type EditTokenSigner interface {
	Token(submissionID uuid.UUID) string
	Verify(submissionID uuid.UUID, token string) bool
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
	SignView(photoID uuid.UUID, exp time.Time) string
	VerifyView(photoID uuid.UUID, exp int64, sig string) bool
}
