package usecase

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// OTPStatusView adalah isi GET /auth/otp/{id}. Tidak memuat data pengguna atau kode.
type OTPStatusView struct {
	Status        domain.OTPStatus // queued / sent / failed / expired / consumed
	FailureReason string
	ResendAfter   time.Duration
	ExpiresAt     *time.Time
}

// decoys mencatat challenge_id palsu yang dibagikan untuk nomor tak terdaftar, agar statusnya bisa
// berjalan seperti challenge asli (queued → sent → expired). Tanpa ini, memantau status akan
// membedakan nomor terdaftar (menjadi sent) dari yang tidak (queued selamanya). Disimpan di memori
// dengan batas jumlah; setelah restart atau terbuang, id palsu terlihat queued (sama seperti id
// yang tidak dikenal).
type decoys struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration // masa berlaku kode tiruan sejak "terkirim"
	items map[uuid.UUID]decoy
	order []uuid.UUID // urutan masuk, untuk membuang yang tertua
}

type decoy struct {
	createdAt, sentAt time.Time
	attempts          int  // tebakan kode yang dicoba (ditiru seperti verify_attempts)
	burned            bool // batas percobaan tercapai: terlihat expired seperti challenge asli
}

func newDecoys(max int, ttl time.Duration) *decoys {
	return &decoys{max: max, ttl: ttl, items: make(map[uuid.UUID]decoy)}
}

// attempt mencatat satu tebakan untuk id palsu dan mengembalikan jumlahnya; setelah max tebakan
// id itu dianggap dibatalkan.
func (d *decoys) attempt(id uuid.UUID, max int) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	v := d.items[id]
	v.attempts++
	v.burned = v.attempts >= max
	d.items[id] = v
	return v.attempts
}

func (d *decoys) add(id uuid.UUID, createdAt, sentAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for len(d.order) >= d.max {
		delete(d.items, d.order[0])
		d.order = d.order[1:]
	}
	d.items[id] = decoy{createdAt: createdAt, sentAt: sentAt}
	d.order = append(d.order, id)
}

// get mengembalikan salinan catatan id palsu.
func (d *decoys) get(id uuid.UUID) (decoy, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.items[id]
	return v, ok
}

// Status mengembalikan status pengiriman challenge. Id yang tidak dikenal menghasilkan queued agar
// tidak membuka informasi.
func (o *OTPRequest) Status(ctx context.Context, id uuid.UUID) (*OTPStatusView, error) {
	now := o.clock.Now()
	c, err := o.otps.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		if d, ok := o.decoys.get(id); ok {
			return o.decoyView(d, now), nil
		}
		return &OTPStatusView{Status: domain.OTPQueued, ResendAfter: o.resendAfter(now, now)}, nil
	}
	if err != nil {
		return nil, err
	}
	v := &OTPStatusView{Status: c.Status, ResendAfter: o.resendAfter(c.CreatedAt, now)}
	switch c.Status {
	case domain.OTPSending:
		v.Status = domain.OTPQueued // detail internal; bagi user masih menunggu
	case domain.OTPSent:
		v.ExpiresAt = c.ExpiresAt
		if c.ExpiresAt != nil && !now.Before(*c.ExpiresAt) {
			v.Status, v.ExpiresAt = domain.OTPExpired, nil
		}
	case domain.OTPFailed:
		v.FailureReason, v.ResendAfter = c.FailureReason, 0
	}
	return v, nil
}

func (o *OTPRequest) decoyView(d decoy, now time.Time) *OTPStatusView {
	v := &OTPStatusView{Status: domain.OTPQueued, ResendAfter: o.resendAfter(d.createdAt, now)}
	if !now.Before(d.sentAt) {
		exp := d.sentAt.Add(o.decoys.ttl)
		v.Status, v.ExpiresAt = domain.OTPSent, &exp
		if d.burned || !now.Before(exp) {
			v.Status, v.ExpiresAt = domain.OTPExpired, nil
		}
	}
	return v
}

func (o *OTPRequest) resendAfter(createdAt, now time.Time) time.Duration {
	return max(0, createdAt.Add(o.cfg.ResendCooldown).Sub(now))
}
