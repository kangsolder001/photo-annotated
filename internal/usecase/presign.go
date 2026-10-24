package usecase

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// PresignConfig adalah parameter usecase presign (diisi dari config di composition root).
type PresignConfig struct {
	PublicBaseURL  string // mis. https://api.example.com (tanpa '/' di akhir)
	MaxUploadBytes int64
	UploadURLTTL   time.Duration
}

// Presign menerbitkan URL upload bertanda tangan dan mencatat foto berstatus pending.
type Presign struct {
	photos PhotoRepo
	store  PhotoStore
	signer URLSigner
	clock  Clock
	cfg    PresignConfig
	newID  func() uuid.UUID
}

func NewPresign(photos PhotoRepo, store PhotoStore, signer URLSigner, clock Clock, cfg PresignConfig) *Presign {
	return &Presign{
		photos: photos, store: store, signer: signer, clock: clock, cfg: cfg,
		newID: func() uuid.UUID {
			id, err := uuid.NewV7() // terurut waktu: indeks PK lebih rapat
			if err != nil {
				panic(err) // hanya gagal bila sumber acak OS rusak
			}
			return id
		},
	}
}

type PresignInput struct {
	ContentType string
	SizeBytes   int64
	Width       int
	Height      int
	OwnerID     *uuid.UUID // nil pada auth.mode = none
}

type PresignOutput struct {
	PhotoID   uuid.UUID
	Method    string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

func (u *Presign) Presign(ctx context.Context, in PresignInput) (*PresignOutput, error) {
	if err := domain.ValidateUploadSpec(in.ContentType, in.SizeBytes, in.Width, in.Height, u.cfg.MaxUploadBytes); err != nil {
		return nil, err
	}
	// Tolak sebelum membuat baris foto jika penyimpanan tidak siap (SSD lepas/penuh).
	if err := u.store.Ready(ctx); err != nil {
		return nil, err
	}

	now := u.clock.Now()
	id := u.newID()
	key, err := domain.NewStorageKey(id, now, in.ContentType)
	if err != nil {
		return nil, err
	}
	photo := &domain.Photo{
		ID: id, OwnerID: in.OwnerID, StorageKey: key, ContentType: in.ContentType,
		SizeBytes: in.SizeBytes, Width: in.Width, Height: in.Height,
		Status: domain.PhotoPending, CreatedAt: now,
	}
	if err := u.photos.Create(ctx, photo); err != nil {
		return nil, err
	}

	exp := now.Add(u.cfg.UploadURLTTL)
	sig := u.signer.SignUpload(id, exp, in.ContentType, in.SizeBytes)
	q := url.Values{"exp": {strconv.FormatInt(exp.Unix(), 10)}, "sig": {sig}}
	return &PresignOutput{
		PhotoID:   id,
		Method:    "PUT",
		URL:       fmt.Sprintf("%s/api/v1/uploads/%s?%s", u.cfg.PublicBaseURL, id, q.Encode()),
		Headers:   map[string]string{"Content-Type": in.ContentType},
		ExpiresAt: exp,
	}, nil
}
