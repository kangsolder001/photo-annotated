package domain

import (
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const (
	ContentTypeWebP = "image/webp"
	ContentTypeJPEG = "image/jpeg"

	// MaxImageSide adalah sisi terpanjang maksimum foto yang dideklarasikan (spec §5.1).
	MaxImageSide = 2048
)

// PhotoStatus adalah status siklus hidup foto (spec §7.2).
type PhotoStatus string

const (
	PhotoPending  PhotoStatus = "pending"  // presign dibuat, belum dipakai submission
	PhotoAttached PhotoStatus = "attached" // sudah dipakai submission
	PhotoRejected PhotoStatus = "rejected" // file tidak lolos validasi
	PhotoDeleted  PhotoStatus = "deleted"  // dihapus (edit/retensi), menunggu pembersihan file
)

func (s PhotoStatus) Valid() bool {
	switch s {
	case PhotoPending, PhotoAttached, PhotoRejected, PhotoDeleted:
		return true
	}
	return false
}

// Photo adalah metadata satu foto. Byte foto ada di PhotoStore, dirujuk lewat StorageKey.
type Photo struct {
	ID           uuid.UUID
	SubmissionID *uuid.UUID // nil selama pending
	OwnerID      *uuid.UUID // nil pada auth.mode = none
	Position     int
	StorageKey   string
	ContentType  string
	SizeBytes    int64 // dideklarasikan saat presign; dicocokkan dengan byte aktual saat upload
	Width        int
	Height       int
	UploadedAt   *time.Time
	SHA256       []byte
	Status       PhotoStatus
	CreatedAt    time.Time
}

// ReadyToAttach memastikan foto boleh dipakai submission: masih pending dan sudah ter-upload.
func (p *Photo) ReadyToAttach() error {
	if p.Status != PhotoPending {
		return Conflict("PHOTO_NOT_AVAILABLE", "photo %s is %s and cannot be attached", p.ID, p.Status)
	}
	if p.UploadedAt == nil {
		return Invalid("PHOTO_NOT_UPLOADED", "photo %s has not been uploaded yet", p.ID)
	}
	return nil
}

// ExtFor mengembalikan ekstensi file untuk content-type yang diizinkan.
func ExtFor(contentType string) (string, bool) {
	switch contentType {
	case ContentTypeWebP:
		return "webp", true
	case ContentTypeJPEG:
		return "jpg", true
	}
	return "", false
}

// ValidateUploadSpec memeriksa deklarasi foto saat presign (spec §5.1): tipe, ukuran, dimensi.
func ValidateUploadSpec(contentType string, sizeBytes int64, width, height int, maxBytes int64) error {
	if _, ok := ExtFor(contentType); !ok {
		return Invalid("INVALID_CONTENT_TYPE", "content_type must be image/webp or image/jpeg")
	}
	if sizeBytes <= 0 || sizeBytes > maxBytes {
		return Invalid("INVALID_SIZE", "size_bytes must be between 1 and %d", maxBytes)
	}
	if width <= 0 || height <= 0 || max(width, height) > MaxImageSide {
		return Invalid("INVALID_DIMENSIONS", "width and height must be positive and the longest side at most %d px", MaxImageSide)
	}
	return nil
}

// NewStorageKey membentuk kunci penyimpanan {yyyy}/{mm}/{dd}/{uuid}.{ext} (UTC). Kunci
// selalu dibuat server dari UUID dan tanggal, tidak pernah dari input klien.
func NewStorageKey(id uuid.UUID, now time.Time, contentType string) (string, error) {
	ext, ok := ExtFor(contentType)
	if !ok {
		return "", Invalid("INVALID_CONTENT_TYPE", "content_type must be image/webp or image/jpeg")
	}
	u := now.UTC()
	return fmt.Sprintf("%04d/%02d/%02d/%s.%s", u.Year(), int(u.Month()), u.Day(), id, ext), nil
}

var storageKeyRe = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(webp|jpg)$`)

// IsValidStorageKey dipakai sebagai pagar terakhir sebelum sebuah kunci menyentuh filesystem.
func IsValidStorageKey(key string) bool { return storageKeyRe.MatchString(key) }
