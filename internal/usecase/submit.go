package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/domain/survey"
)

// SubmitConfig adalah batas-batas validasi submission (dari config limits.*).
type SubmitConfig struct {
	MaxPhotos              int
	MaxAnnotationsPerPhoto int
}

// Submit menyimpan submission baru: foto yang sudah ter-upload + anotasinya (spec §5.2).
type Submit struct {
	photos     PhotoRepo
	subs       SubmissionRepo
	labels     LabelRepo
	tx         TxManager
	store      PhotoStore
	editTokens EditTokenSigner // nil bila tidak dipakai (auth.mode = otp)
	clock      Clock
	cfg        SubmitConfig
	newID      func() uuid.UUID
}

func NewSubmit(photos PhotoRepo, subs SubmissionRepo, labels LabelRepo, tx TxManager, store PhotoStore, editTokens EditTokenSigner, clock Clock, cfg SubmitConfig) *Submit {
	return &Submit{photos: photos, subs: subs, labels: labels, tx: tx, store: store, editTokens: editTokens, clock: clock, cfg: cfg, newID: newV7}
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
	// Jawaban pendataan mentah dari klien (spec-pendataan §5.3); divalidasi paket survey.
	FormVersion     int
	Answers         map[string]json.RawMessage
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
	// 1. Bentuk request (murah, tanpa I/O): jawaban pendataan lalu foto
	answers, anns, err := u.validateShape(in)
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
		Version: 1, CreatedAt: now, UpdatedAt: now, Review: domain.Review{Status: domain.ReviewPending},
		FormVersion: in.FormVersion, Answers: answers,
	}
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		// label dibaca FOR SHARE di transaksi yang sama: arsip/hapus berbarengan tidak bisa lolos
		if err := checkLabels(ctx, u.labels, anns, nil); err != nil {
			return err
		}
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

// validateShape memvalidasi request submission baru dan mengembalikan jawaban ternormalisasi serta
// anotasi yang sudah dinormalisasi (indeks sejajar dengan in.Photos).
func (u *Submit) validateShape(in SubmitInput) (survey.Answers, [][]domain.Annotation, error) {
	if in.ClientRequestID == uuid.Nil {
		return nil, nil, domain.Invalid("INVALID_CLIENT_REQUEST_ID", "client_request_id is required")
	}
	return validateSurvey(u.cfg, in.FormVersion, in.Answers, in.Photos, u.clock.Now())
}

// validateSurvey memvalidasi jawaban pendataan (spec-pendataan §4) lalu foto sesuai persetujuan
// (dipakai Submit dan Edit).
func validateSurvey(cfg SubmitConfig, version int, raw map[string]json.RawMessage, photos []SubmitPhoto, now time.Time) (survey.Answers, [][]domain.Annotation, error) {
	answers, err := survey.Normalize(version, raw, now)
	if err != nil {
		return nil, nil, err
	}
	anns, err := validatePhotos(cfg, photos, answers[survey.ConsentID] == survey.ConsentYes)
	return answers, anns, err
}

// validatePhotos memvalidasi daftar foto beserta anotasinya (dipakai Submit dan Edit): 1..N foto bila
// responden bersedia, 0 bila menolak (spec-pendataan §4.3), tanpa duplikat, 0..M anotasi per foto, tiap
// anotasi lolos aturan domain. Mengembalikan anotasi yang sudah dinormalisasi dengan indeks sejajar
// dengan photos.
func validatePhotos(cfg SubmitConfig, photos []SubmitPhoto, consented bool) ([][]domain.Annotation, error) {
	n := len(photos)
	if consented && (n < 1 || n > cfg.MaxPhotos) {
		return nil, domain.Invalid("INVALID_PHOTO_COUNT", "a consenting household needs between 1 and %d photos", cfg.MaxPhotos)
	}
	if !consented && n != 0 {
		return nil, domain.Invalid("INVALID_PHOTO_COUNT", "photos must be empty when the respondent declines")
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
			v, err := a.Validate()
			if err != nil {
				return nil, withPath(err, fmt.Sprintf("photos[%d].annotations[%d]", i, j))
			}
			out[i][j] = v
		}
	}
	return out, nil
}

// checkLabels memastikan setiap kotak memakai label yang ada dan aktif (spec-labels-admin §4.2). Wajib
// dipanggil di dalam transaksi submit/edit. keepArchived (boleh nil) mengizinkan label arsip untuk kotak
// pada foto ke-i, dipakai edit agar kotak lama yang labelnya diarsipkan tetap bisa disimpan ulang.
// Error membawa details label_id + indeks foto/kotak agar FE bisa menandai kotak yang perlu diganti.
func checkLabels(ctx context.Context, repo LabelRepo, anns [][]domain.Annotation, keepArchived func(photo, labelID int) bool) error {
	var ids []int
	seen := map[int]bool{}
	for _, list := range anns {
		for _, a := range list {
			if !seen[a.LabelID] {
				seen[a.LabelID] = true
				ids = append(ids, a.LabelID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	found, err := repo.ForShare(ctx, ids)
	if err != nil {
		return err
	}
	for i, list := range anns {
		for j, a := range list {
			details := map[string]any{"label_id": a.LabelID, "photo_index": i, "annotation_index": j}
			path := fmt.Sprintf("photos[%d].annotations[%d]", i, j)
			l, ok := found[a.LabelID]
			if !ok {
				return withPath(domain.Invalid("INVALID_LABEL", "label %d does not exist", a.LabelID).WithDetails(details), path)
			}
			if l.Archived() && (keepArchived == nil || !keepArchived(i, a.LabelID)) {
				return withPath(domain.Invalid("LABEL_ARCHIVED", "label %d is archived; choose another label", a.LabelID).WithDetails(details), path)
			}
		}
	}
	return nil
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
