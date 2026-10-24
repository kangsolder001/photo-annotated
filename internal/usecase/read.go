package usecase

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// ReadConfig adalah parameter usecase baca.
type ReadConfig struct {
	PublicBaseURL string
	EditWindow    time.Duration
	ViewURLTTL    time.Duration
}

// Reader menangani pembacaan submission dan foto (spec §5.3–5.5).
type Reader struct {
	subs   SubmissionRepo
	photos PhotoRepo
	store  PhotoStore
	signer URLSigner
	authz  authorizer
	clock  Clock
	cfg    ReadConfig
}

func NewReader(subs SubmissionRepo, photos PhotoRepo, store PhotoStore, signer URLSigner, editTokens EditTokenSigner, clock Clock, cfg ReadConfig) *Reader {
	return &Reader{subs: subs, photos: photos, store: store, signer: signer, authz: authorizer{editTokens}, clock: clock, cfg: cfg}
}

// SubmissionView adalah submission untuk klien: detail + status keterbukaan edit.
type SubmissionView struct {
	Detail        domain.SubmissionDetail
	EditableUntil time.Time // created_at + jendela edit
	Editable      bool      // dihitung dengan jam server, bukan jam klien
}

// Submission mengembalikan submission lengkap. Tidak ada kredensial yang cocok, atau id tidak
// ada, keduanya dijawab domain.ErrNotFound sehingga keberadaan submission tidak bisa ditebak.
func (u *Reader) Submission(ctx context.Context, c Caller, id uuid.UUID) (*SubmissionView, error) {
	sub, err := u.subs.Get(ctx, id)
	if err != nil {
		return nil, err // ErrNotFound atau kegagalan DB
	}
	if !u.authz.canRead(c, sub) {
		return nil, notFound()
	}
	d, err := u.subs.GetDetail(ctx, id) // snapshot konsisten (satu statement)
	if err != nil {
		return nil, err
	}
	until := d.Submission.CreatedAt.Add(u.cfg.EditWindow)
	return &SubmissionView{Detail: *d, EditableUntil: until, Editable: u.clock.Now().Before(until)}, nil
}

func notFound() error { return fmt.Errorf("%w", domain.ErrNotFound) }

type PhotoURL struct {
	URL       string
	ExpiresAt time.Time
}

// PhotoURL menerbitkan URL bertanda tangan berumur pendek untuk melihat foto. Otorisasi
// dicek di sini (saat menerbitkan); /content hanya memeriksa tanda tangan agar bisa dipakai di <img>.
func (u *Reader) PhotoURL(ctx context.Context, c Caller, photoID uuid.UUID) (*PhotoURL, error) {
	photo, err := u.photos.Get(ctx, photoID)
	if err != nil {
		return nil, err
	}
	// hanya foto yang sudah dipakai submission yang bisa dilihat lewat jalur ini
	if photo.Status != domain.PhotoAttached || photo.SubmissionID == nil {
		return nil, notFound()
	}
	sub, err := u.subs.Get(ctx, *photo.SubmissionID)
	if err != nil {
		return nil, err
	}
	if !u.authz.canRead(c, sub) {
		return nil, notFound()
	}
	exp := u.clock.Now().Add(u.cfg.ViewURLTTL)
	return &PhotoURL{URL: viewURL(u.cfg.PublicBaseURL, u.signer, photo.ID, exp), ExpiresAt: exp}, nil
}

// viewURL membangun URL /content bertanda tangan untuk foto (dipakai Reader dan Export).
func viewURL(base string, signer URLSigner, photoID uuid.UUID, exp time.Time) string {
	q := url.Values{"exp": {strconv.FormatInt(exp.Unix(), 10)}, "sig": {signer.SignView(photoID, exp)}}
	return fmt.Sprintf("%s/api/v1/photos/%s/content?%s", base, photoID, q.Encode())
}

// PhotoContentResult adalah file foto siap disajikan. Pemanggil wajib menutup File.
type PhotoContentResult struct {
	File        File
	ContentType string
	ETag        string // `"<sha256 hex>"`; isi foto tidak berubah setelah attached
}

var (
	errViewSignature = domain.Forbidden("INVALID_SIGNATURE", "photo URL is not valid")
	errViewExpired   = domain.Gone("URL_EXPIRED", "photo URL has expired; request a new one")
)

// PhotoContent memverifikasi URL lihat foto lalu membuka filenya. Id yang tak dikenal dijawab
// sama dengan tanda tangan salah (tidak bisa dipakai untuk menebak keberadaan foto).
func (u *Reader) PhotoContent(ctx context.Context, photoID uuid.UUID, exp int64, sig string) (*PhotoContentResult, error) {
	// Tanda tangan view hanya bergantung pada id dan exp: periksa sebelum menyentuh DB, sehingga
	// request acak ke rute publik ini tidak membebani database.
	if !u.signer.VerifyView(photoID, exp, sig) {
		return nil, errViewSignature
	}
	if u.clock.Now().Unix() > exp {
		return nil, errViewExpired
	}
	photo, err := u.photos.Get(ctx, photoID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, notFound() // tanda tangan sah, tetapi foto sudah dihapus
	}
	if err != nil {
		return nil, err
	}
	if photo.Status != domain.PhotoAttached {
		return nil, notFound() // dihapus (edit/retensi) setelah URL diterbitkan
	}
	f, err := u.store.Open(ctx, photo.StorageKey)
	if errors.Is(err, ErrFileNotFound) {
		return nil, domain.NotFound("PHOTO_FILE_MISSING", "the photo file is no longer available")
	}
	if err != nil {
		return nil, err
	}
	res := &PhotoContentResult{File: f, ContentType: photo.ContentType}
	if len(photo.SHA256) > 0 {
		res.ETag = `"` + hex.EncodeToString(photo.SHA256) + `"`
	}
	return res, nil
}
