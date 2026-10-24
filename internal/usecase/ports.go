// Package usecase berisi logika aplikasi. Paket ini hanya mengenal domain dan interface
// (port) yang didefinisikan di sini; adapter di luar mengimplementasikannya.
package usecase

import (
	"context"
	"errors"
	"io"
	"time"
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
