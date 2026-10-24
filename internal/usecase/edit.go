package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// EditConfig adalah parameter usecase edit.
type EditConfig struct {
	SubmitConfig
	Window time.Duration // jendela edit sejak created_at (spec §4.4, default 24 jam)
}

// Edit mengganti isi submission yang sudah ada (spec §5.4): daftar foto dan anotasinya menjadi
// keadaan akhir yang dikirim klien, dengan optimistic locking lewat version.
type Edit struct {
	photos PhotoRepo
	subs   SubmissionRepo
	labels LabelRepo
	tx     TxManager
	gate   photoGate
	authz  authorizer
	clock  Clock
	cfg    EditConfig
}

func NewEdit(photos PhotoRepo, subs SubmissionRepo, labels LabelRepo, tx TxManager, store PhotoStore, editTokens EditTokenSigner, clock Clock, cfg EditConfig) *Edit {
	return &Edit{
		photos: photos, subs: subs, labels: labels, tx: tx, gate: photoGate{photos: photos, store: store},
		authz: authorizer{editTokens}, clock: clock, cfg: cfg,
	}
}

type EditInput struct {
	SubmissionID uuid.UUID
	Caller       Caller
	Version      int // version yang terakhir dilihat klien
	Photos       []SubmitPhoto
}

type EditOutput struct {
	SubmissionID uuid.UUID
	Version      int
	UpdatedAt    time.Time
}

func (u *Edit) Edit(ctx context.Context, in EditInput) (*EditOutput, error) {
	// 1. Bentuk request
	if in.Version < 1 {
		return nil, domain.Invalid("INVALID_VERSION", "version must be the version you last loaded (>= 1)")
	}
	anns, err := validatePhotos(u.cfg.SubmitConfig, in.Photos)
	if err != nil {
		return nil, err
	}

	// 2. Otorisasi. Tanpa hak baca (atau id tak ada) → 404 seragam. Yang boleh membaca tapi
	//    tidak boleh mengedit (admin) tahu submission itu ada, jadi diberi 403 yang jelas.
	sub, err := u.subs.Get(ctx, in.SubmissionID)
	if err != nil {
		return nil, err
	}
	if !u.authz.canEdit(in.Caller, sub) {
		if u.authz.canRead(in.Caller, sub) {
			return nil, domain.Forbidden("EDIT_NOT_ALLOWED", "only the owner can edit a submission")
		}
		return nil, notFound()
	}

	// 3. Jendela edit: dihitung dari created_at dan tidak diperpanjang oleh edit.
	now := u.clock.Now().UTC().Truncate(time.Microsecond)
	if !now.Before(sub.CreatedAt.Add(u.cfg.Window)) {
		return nil, domain.Forbidden("EDIT_WINDOW_CLOSED", "the edit window for this submission has closed")
	}

	// 4. Foto: yang sudah attached ke submission ini boleh dipertahankan tanpa pemeriksaan ulang;
	//    foto baru diperiksa seperti pada submission baru (pemilik, upload, file di disk).
	current, err := u.photos.ListBySubmission(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	existing := make(map[uuid.UUID]*domain.Photo, len(current))
	for _, p := range current {
		existing[p.ID] = p
	}
	var fresh []gateItem
	for i, p := range in.Photos {
		if _, kept := existing[p.PhotoID]; !kept {
			fresh = append(fresh, gateItem{Index: i, PhotoID: p.PhotoID})
		}
	}
	freshPhotos, err := u.gate.load(ctx, fresh, sub.OwnerID)
	if err != nil {
		return nil, err
	}
	if err := u.gate.checkFiles(ctx, fresh, freshPhotos); err != nil {
		return nil, err
	}

	// 5. Satu transaksi. BumpVersion lebih dulu: ia mengunci baris submission sampai commit,
	//    sehingga dua edit serentak diserialkan dan yang basi gagal dengan VERSION_CONFLICT.
	requested := make(map[uuid.UUID]struct{}, len(in.Photos))
	for _, p := range in.Photos {
		requested[p.PhotoID] = struct{}{}
	}
	var newVersion int
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		v, err := u.subs.BumpVersion(ctx, sub.ID, in.Version, now)
		if err != nil {
			return err
		}
		newVersion = v
		// Label arsip tetap boleh untuk kotak pada foto yang di versi sebelumnya sudah memakai label itu
		// (spec-labels-admin §4.2): pengguna tidak terkunci dari submission miliknya hanya karena admin
		// mengarsipkan label di tengah jendela edit. Dibaca setelah BumpVersion (baris submission terkunci).
		kept := make([]uuid.UUID, 0, len(current))
		for _, p := range current {
			kept = append(kept, p.ID)
		}
		prev, err := u.subs.PhotoLabelIDs(ctx, kept)
		if err != nil {
			return err
		}
		keepArchived := func(i, labelID int) bool { return prev[in.Photos[i].PhotoID][labelID] }
		if err := checkLabels(ctx, u.labels, anns, keepArchived); err != nil {
			return err
		}
		// foto yang dibuang: tandai deleted dan hapus anotasinya sekarang (filenya oleh cleanup)
		for _, p := range current {
			if _, keep := requested[p.ID]; keep {
				continue
			}
			if err := u.photos.MarkDeleted(ctx, p.ID, sub.ID); err != nil {
				return err
			}
			if err := u.subs.ReplaceAnnotations(ctx, p.ID, nil); err != nil {
				return err
			}
		}
		for i, p := range in.Photos {
			if _, kept := existing[p.PhotoID]; kept {
				if err := u.photos.SetPosition(ctx, p.PhotoID, sub.ID, i); err != nil {
					return err
				}
				if err := u.subs.ReplaceAnnotations(ctx, p.PhotoID, anns[i]); err != nil {
					return err
				}
				continue
			}
			if err := u.photos.Attach(ctx, p.PhotoID, sub.ID, i, sub.OwnerID); err != nil {
				return err
			}
			if err := u.subs.AddAnnotations(ctx, p.PhotoID, anns[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &EditOutput{SubmissionID: sub.ID, Version: newVersion, UpdatedAt: now}, nil
}
