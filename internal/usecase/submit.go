package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// SubmitConfig adalah batas-batas validasi submission (dari config limits.*).
type SubmitConfig struct {
	MaxPhotos              int
	MaxAnnotationsPerPhoto int
	MaxAnnotationTextLen   int
}

// Submit menyimpan submission baru: foto yang sudah ter-upload + anotasinya (spec §5.2).
type Submit struct {
	photos     PhotoRepo
	subs       SubmissionRepo
	tx         TxManager
	store      PhotoStore
	editTokens EditTokenSigner // nil bila tidak dipakai (auth.mode = otp)
	clock      Clock
	cfg        SubmitConfig
	newID      func() uuid.UUID
}

func NewSubmit(photos PhotoRepo, subs SubmissionRepo, tx TxManager, store PhotoStore, editTokens EditTokenSigner, clock Clock, cfg SubmitConfig) *Submit {
	return &Submit{photos: photos, subs: subs, tx: tx, store: store, editTokens: editTokens, clock: clock, cfg: cfg, newID: newV7}
}

func newV7() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err) // hanya gagal bila sumber acak OS rusak
	}
	return id
}

type SubmitPhoto struct {
	PhotoID     uuid.UUID
	Annotations []domain.Annotation
}

type SubmitInput struct {
	Photos          []SubmitPhoto
	ClientRequestID uuid.UUID
	OwnerID         *uuid.UUID  // nil pada auth.mode = none
	ClientIP        *netip.Addr // untuk catatan; boleh nil
}

type SubmitOutput struct {
	SubmissionID uuid.UUID
	Version      int
	CreatedAt    time.Time
	EditToken    string // kosong bila tidak berlaku
	Replayed     bool   // true bila ini permintaan ulang dari client_request_id yang sama
}

func (u *Submit) Submit(ctx context.Context, in SubmitInput) (*SubmitOutput, error) {
	// 1. Bentuk request (murah, tanpa I/O)
	anns, err := u.validateShape(in)
	if err != nil {
		return nil, err
	}

	// 2. Idempotensi: permintaan yang sama dijawab dengan submission yang sudah ada
	if out, err := u.replay(ctx, in); err != nil || out != nil {
		return out, err
	}

	// 3. Muat dan periksa semua foto SEBELUM transaksi (I/O disk tidak boleh menahan transaksi)
	items := make([]gateItem, len(in.Photos))
	for i, p := range in.Photos {
		items[i] = gateItem{Index: i, PhotoID: p.PhotoID}
	}
	gate := photoGate{photos: u.photos, store: u.store}
	photos, err := gate.load(ctx, items, in.OwnerID)
	if err != nil {
		return u.replayOnConflict(ctx, in, err)
	}
	if err := gate.checkFiles(ctx, items, photos); err != nil {
		return nil, err
	}

	// 4. Satu transaksi: submission + attach foto + anotasi
	// Dipotong ke mikrodetik (presisi Postgres) agar respons pertama dan replay identik.
	now := u.clock.Now().UTC().Truncate(time.Microsecond)
	sub := &domain.Submission{
		ID: u.newID(), OwnerID: in.OwnerID, ClientRequestID: in.ClientRequestID, ClientIP: in.ClientIP,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.subs.Create(ctx, sub); err != nil {
			return err
		}
		for i, p := range in.Photos {
			if err := u.photos.Attach(ctx, p.PhotoID, sub.ID, i, in.OwnerID); err != nil {
				return err
			}
			if err := u.subs.AddAnnotations(ctx, p.PhotoID, anns[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return u.replayOnConflict(ctx, in, err)
	}
	return u.output(sub, false), nil
}

// replayOnConflict menangani balapan dua request dengan client_request_id yang sama. Yang kalah
// bisa gagal di beberapa titik: pada UNIQUE client_request_id, atau karena fotonya sudah
// di-attach oleh yang menang. Pada kegagalan jenis konflik, baca ulang di luar transaksi:
// bila submission untuk client_request_id ini sudah ada, jawab sebagai replay; bila belum,
// kembalikan error aslinya.
func (u *Submit) replayOnConflict(ctx context.Context, in SubmitInput, cause error) (*SubmitOutput, error) {
	if de, ok := domain.AsError(cause); ok && de.Kind == domain.KindConflict {
		if out, err := u.replay(ctx, in); err != nil || out != nil {
			return out, err
		}
	}
	return nil, cause
}

var errDuplicateRequest = domain.Conflict("DUPLICATE_CLIENT_REQUEST", "a submission with this client_request_id already exists")

// validateShape memvalidasi request submission baru dan mengembalikan anotasi yang sudah
// dinormalisasi (indeks sejajar dengan in.Photos).
func (u *Submit) validateShape(in SubmitInput) ([][]domain.Annotation, error) {
	if in.ClientRequestID == uuid.Nil {
		return nil, domain.Invalid("INVALID_CLIENT_REQUEST_ID", "client_request_id is required")
	}
	return validatePhotos(u.cfg, in.Photos)
}

// validatePhotos memvalidasi daftar foto beserta anotasinya (dipakai Submit dan Edit): 1..N foto,
// tanpa duplikat, 0..M anotasi per foto, tiap anotasi lolos aturan domain. Mengembalikan anotasi
// yang sudah dinormalisasi dengan indeks sejajar dengan photos.
func validatePhotos(cfg SubmitConfig, photos []SubmitPhoto) ([][]domain.Annotation, error) {
	if n := len(photos); n < 1 || n > cfg.MaxPhotos {
		return nil, domain.Invalid("INVALID_PHOTO_COUNT", "a submission needs between 1 and %d photos", cfg.MaxPhotos)
	}
	seen := make(map[uuid.UUID]struct{}, len(photos))
	out := make([][]domain.Annotation, len(photos))
	for i, p := range photos {
		if p.PhotoID == uuid.Nil {
			return nil, domain.Invalid("PHOTO_NOT_FOUND", "photos[%d].photo_id is required", i)
		}
		if _, dup := seen[p.PhotoID]; dup {
			return nil, domain.Invalid("DUPLICATE_PHOTO", "photos[%d]: photo %s is listed more than once", i, p.PhotoID)
		}
		seen[p.PhotoID] = struct{}{}
		if len(p.Annotations) > cfg.MaxAnnotationsPerPhoto {
			return nil, domain.Invalid("TOO_MANY_ANNOTATIONS", "photos[%d]: at most %d annotations per photo", i, cfg.MaxAnnotationsPerPhoto)
		}
		out[i] = make([]domain.Annotation, len(p.Annotations))
		for j, a := range p.Annotations {
			v, err := a.Validate(cfg.MaxAnnotationTextLen)
			if err != nil {
				return nil, withPath(err, fmt.Sprintf("photos[%d].annotations[%d]", i, j))
			}
			out[i][j] = v
		}
	}
	return out, nil
}

// withPath menambahkan lokasi field ke pesan error domain tanpa mengubah Kind/Code.
func withPath(err error, path string) error {
	if de, ok := domain.AsError(err); ok {
		c := *de
		c.Message = path + ": " + de.Message
		return &c
	}
	return err
}

// replay mengembalikan hasil submission yang sudah ada untuk client_request_id ini (atau nil, nil).
func (u *Submit) replay(ctx context.Context, in SubmitInput) (*SubmitOutput, error) {
	existing, err := u.subs.GetByClientRequestID(ctx, in.ClientRequestID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// ID dipakai orang lain: jangan membocorkan submission itu.
	if !sameOwner(existing.OwnerID, in.OwnerID) {
		return nil, errDuplicateRequest
	}
	return u.output(existing, true), nil
}

func sameOwner(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (u *Submit) output(s *domain.Submission, replayed bool) *SubmitOutput {
	out := &SubmitOutput{SubmissionID: s.ID, Version: s.Version, CreatedAt: s.CreatedAt, Replayed: replayed}
	if u.editTokens != nil {
		out.EditToken = u.editTokens.Token(s.ID)
	}
	return out
}

// gateItem menunjuk satu foto pada request beserta indeksnya (untuk pesan error "photos[i]").
type gateItem struct {
	Index   int
	PhotoID uuid.UUID
}

// photoGate memeriksa foto yang akan dilampirkan ke submission (dipakai Submit dan Edit).
type photoGate struct {
	photos PhotoRepo
	store  PhotoStore
}

// load memuat setiap foto dan memastikan siap dipakai oleh owner. Foto milik orang lain
// dijawab seolah tidak ada, supaya keberadaannya tidak bisa ditebak.
func (g photoGate) load(ctx context.Context, items []gateItem, owner *uuid.UUID) ([]*domain.Photo, error) {
	out := make([]*domain.Photo, len(items))
	for k, it := range items {
		p, err := g.photos.Get(ctx, it.PhotoID)
		if errors.Is(err, domain.ErrNotFound) || (err == nil && !sameOwner(p.OwnerID, owner)) {
			return nil, domain.Invalid("PHOTO_NOT_FOUND", "photos[%d]: photo %s does not exist", it.Index, it.PhotoID)
		}
		if err != nil {
			return nil, err
		}
		if err := p.ReadyToAttach(); err != nil {
			return nil, withPath(err, fmt.Sprintf("photos[%d]", it.Index))
		}
		out[k] = p
	}
	return out, nil
}

// checkFiles memastikan file tiap foto benar-benar ada di penyimpanan dengan ukuran yang
// dideklarasikan (Stat saja, tanpa membaca isi). Foto yang bermasalah ditandai rejected;
// kegagalan penyimpanan itu sendiri bukan salah foto dan dikembalikan apa adanya.
func (g photoGate) checkFiles(ctx context.Context, items []gateItem, photos []*domain.Photo) error {
	for k, p := range photos {
		info, err := g.store.Stat(ctx, p.StorageKey)
		switch {
		case err == nil && info.Size == p.SizeBytes:
			continue
		case err != nil && !errors.Is(err, ErrFileNotFound):
			return err
		}
		fctx, cancel := finalizeCtx(ctx)
		mErr := g.photos.MarkRejected(fctx, p.ID)
		cancel()
		bad := domain.Invalid("PHOTO_FILE_INVALID", "photos[%d]: the uploaded file is missing or does not match its declared size; upload the photo again", items[k].Index)
		if mErr != nil {
			return errors.Join(bad, mErr)
		}
		return bad
	}
	return nil
}
