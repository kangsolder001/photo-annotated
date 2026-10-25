package usecase

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

const (
	// finalizeTimeout membatasi tahap akhir (commit + catat DB) yang sengaja tidak ikut
	// dibatalkan saat klien memutus koneksi, supaya file dan baris DB tidak berselisih.
	finalizeTimeout = 10 * time.Second
	busyRetryAfter  = 2 * time.Second
)

var (
	errInvalidSignature = domain.Forbidden("INVALID_SIGNATURE", "upload URL is not valid")
	errURLExpired       = domain.Gone("URL_EXPIRED", "upload URL has expired; request a new one")
)

// UploadConfig adalah parameter usecase upload.
type UploadConfig struct {
	MaxConcurrent int // jumlah upload yang boleh berjalan bersamaan (slot)
}

// Upload menerima byte foto lewat URL bertanda tangan dari Presign: memverifikasi, men-stream
// ke penyimpanan, memeriksa isi, lalu menerbitkan file dan mencatatnya (spec §5.1b).
type Upload struct {
	photos    PhotoRepo
	store     PhotoStore
	signer    URLSigner
	inspector ImageInspector
	clock     Clock

	slots chan struct{}
	locks keyedLock
}

func NewUpload(photos PhotoRepo, store PhotoStore, signer URLSigner, inspector ImageInspector, clock Clock, cfg UploadConfig) *Upload {
	if cfg.MaxConcurrent < 1 {
		panic("usecase: UploadConfig.MaxConcurrent harus >= 1")
	}
	return &Upload{
		photos: photos, store: store, signer: signer, inspector: inspector, clock: clock,
		slots: make(chan struct{}, cfg.MaxConcurrent),
		locks: keyedLock{held: map[uuid.UUID]struct{}{}},
	}
}

type UploadInput struct {
	PhotoID       uuid.UUID
	Exp           int64  // unix detik dari query ?exp=
	Sig           string // dari query ?sig=
	ContentType   string // header Content-Type apa adanya
	ContentLength int64  // header Content-Length; < 0 jika tidak diketahui (chunked)
	Body          io.Reader
}

// Slots mengembalikan jumlah upload yang sedang berjalan dan batasnya (untuk metrik).
func (u *Upload) Slots() (inUse, capacity int) { return len(u.slots), cap(u.slots) }

func (u *Upload) Upload(ctx context.Context, in UploadInput) error {
	// 1-3. Otorisasi lebih dulu. Id yang tak dikenal dijawab sama dengan tanda tangan salah,
	//      sehingga keberadaan foto tidak bisa ditebak.
	photo, err := u.photos.Get(ctx, in.PhotoID)
	if errors.Is(err, domain.ErrNotFound) {
		return errInvalidSignature
	}
	if err != nil {
		return err
	}
	if !u.signer.VerifyUpload(photo.ID, in.Exp, photo.ContentType, photo.SizeBytes, in.Sig) {
		return errInvalidSignature
	}
	if u.clock.Now().Unix() > in.Exp {
		return errURLExpired
	}

	// 4. Status foto
	if photo.Status != domain.PhotoPending {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo is %s and no longer accepts uploads", photo.Status)
	}

	// 5. Header harus cocok dengan deklarasi saat presign
	if in.ContentType != photo.ContentType {
		return domain.Invalid("INVALID_CONTENT_TYPE", "Content-Type must be %s", photo.ContentType)
	}
	if in.ContentLength < 0 {
		return domain.LengthRequired("LENGTH_REQUIRED", "Content-Length header is required (chunked uploads are not accepted)")
	}
	if in.ContentLength != photo.SizeBytes {
		return domain.Invalid("SIZE_MISMATCH", "Content-Length %d does not match the declared size %d", in.ContentLength, photo.SizeBytes)
	}

	// 6. Slot upload: tidak mengantre; penuh → klien mencoba lagi (backoff).
	select {
	case u.slots <- struct{}{}:
		defer func() { <-u.slots }()
	default:
		return domain.Unavailable("UPLOAD_BUSY", "too many uploads in progress, retry shortly").WithRetryAfter(busyRetryAfter)
	}

	// 7. Satu upload per foto pada satu waktu: menjaga hash di DB selalu cocok dengan file.
	if !u.locks.tryLock(photo.ID) {
		return domain.Conflict("UPLOAD_IN_PROGRESS", "another upload of this photo is in progress").WithRetryAfter(busyRetryAfter)
	}
	defer u.locks.unlock(photo.ID)

	// 7b. Baca ulang di bawah kunci. Hanya upload (yang berjalan di bawah kunci ini) yang mengisi
	//     uploaded_at, dan Attach mensyaratkan uploaded_at terisi, jadi foto yang terlihat belum
	//     ter-upload di sini belum mungkin dipakai submission. Foto yang sudah ter-upload tidak
	//     pernah ditimpa: isinya mungkin sudah dipakai submission (dan dianotasi) sejak pembacaan
	//     pertama di atas.
	photo, err = u.photos.Get(ctx, photo.ID)
	if err != nil {
		return err
	}
	if photo.Status != domain.PhotoPending {
		return domain.Conflict("PHOTO_NOT_AVAILABLE", "photo is %s and no longer accepts uploads", photo.Status)
	}

	// 8. Penyimpanan siap (SSD ada, sisa ruang cukup)
	if err := u.store.Ready(ctx); err != nil {
		return err
	}

	// 9. Stream ke area sementara
	staged, err := u.store.Stage(ctx, photo.StorageKey, in.Body, photo.SizeBytes)
	if err != nil {
		return mapStageError(err)
	}
	defer func() { _ = staged.Discard() }() // aman setelah Commit

	if staged.Size() != photo.SizeBytes {
		return domain.Invalid("SIZE_MISMATCH", "received %d bytes but %d were declared", staged.Size(), photo.SizeBytes)
	}

	// 9b. Sudah ter-upload: isi yang sama = retry idempoten (mis. respons pertama hilang di jaringan);
	//     isi berbeda ditolak tanpa menyentuh file maupun status foto.
	if photo.UploadedAt != nil {
		if bytes.Equal(staged.SHA256(), photo.SHA256) {
			return nil
		}
		return domain.Conflict("PHOTO_ALREADY_UPLOADED", "this photo was already uploaded with different content; request a new upload URL")
	}

	// 10. Periksa isi: magic bytes + header, dimensi harus sama dengan deklarasi
	if err := u.inspect(staged, photo); err != nil {
		fctx, cancel := finalizeCtx(ctx)
		defer cancel()
		if mErr := u.photos.MarkRejected(fctx, photo.ID); mErr != nil {
			return errors.Join(err, mErr)
		}
		return err
	}

	// 11-12. Terbitkan file lalu catat. Urutan ini disengaja: bila proses mati di antaranya,
	//        yang tersisa hanyalah file yatim (disapu cleanup), bukan baris DB tanpa file.
	fctx, cancel := finalizeCtx(ctx)
	defer cancel()
	if err := staged.Commit(); err != nil {
		return err
	}
	return u.photos.MarkUploaded(fctx, photo.ID, u.clock.Now(), staged.SHA256())
}

func (u *Upload) inspect(staged StagedFile, photo *domain.Photo) error {
	rc, err := staged.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	info, err := u.inspector.Inspect(rc, photo.ContentType)
	if err != nil {
		return err
	}
	if info.Width != photo.Width || info.Height != photo.Height {
		return domain.Invalid("INVALID_IMAGE", "image is %dx%d but %dx%d was declared", info.Width, info.Height, photo.Width, photo.Height)
	}
	return nil
}

// mapStageError menerjemahkan kegagalan membaca body menjadi error yang bermakna bagi klien.
func mapStageError(err error) error {
	if _, ok := domain.AsError(err); ok {
		return err // SIZE_MISMATCH, STORAGE_FULL, STORAGE_UNAVAILABLE, ...
	}
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF):
		return domain.Invalid("SIZE_MISMATCH", "request body is shorter than declared")
	case errors.Is(err, os.ErrDeadlineExceeded):
		return domain.Timeout("UPLOAD_TIMEOUT", "upload was too slow and timed out")
	}
	return err // klien memutus koneksi dsb.; dicatat di log
}

// finalizeCtx melepas pembatalan dari ctx (klien putus) tetapi tetap dibatasi waktu.
func finalizeCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), finalizeTimeout)
}

// keyedLock adalah kunci non-blocking per UUID (cukup untuk satu instance API).
type keyedLock struct {
	mu   sync.Mutex
	held map[uuid.UUID]struct{}
}

func (k *keyedLock) tryLock(id uuid.UUID) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, busy := k.held[id]; busy {
		return false
	}
	k.held[id] = struct{}{}
	return true
}

func (k *keyedLock) unlock(id uuid.UUID) {
	k.mu.Lock()
	delete(k.held, id)
	k.mu.Unlock()
}
