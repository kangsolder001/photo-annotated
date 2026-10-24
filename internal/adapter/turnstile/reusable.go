package turnstile

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"photo-annotated/internal/usecase"
)

// Reusable membungkus verifier agar satu token Turnstile yang sudah lolos boleh dipakai beberapa
// kali dalam waktu singkat dari IP yang sama. Cloudflare menolak token yang diverifikasi dua kali,
// padahal pada auth.mode = none satu submission butuh beberapa presign (paralel) ditambah submit;
// tanpa ini FE harus menghasilkan token baru untuk setiap request.
//
// Token pertama kali diverifikasi tepat sekali walau dipakai beberapa request berbarengan
// (request lain menunggu hasilnya). Hasil gagal tidak disimpan.
type Reusable struct {
	inner    usecase.TurnstileVerifier
	uses     int
	ttl      time.Duration
	maxItems int
	now      func() time.Time

	mu        sync.Mutex
	entries   map[[32]byte]*reuseEntry
	lastSweep time.Time
}

type reuseEntry struct {
	done    chan struct{} // ditutup saat verifikasi pertama selesai
	err     error
	ip      string
	left    int
	expires time.Time
}

var _ usecase.TurnstileVerifier = (*Reusable)(nil)

// NewReusable: setiap token lolos boleh dipakai `uses` kali (termasuk yang pertama) selama ttl.
func NewReusable(inner usecase.TurnstileVerifier, uses int, ttl time.Duration) *Reusable {
	return NewReusableWithClock(inner, uses, ttl, time.Now)
}

// NewReusableWithClock seperti NewReusable dengan jam yang dapat diatur (untuk test).
func NewReusableWithClock(inner usecase.TurnstileVerifier, uses int, ttl time.Duration, now func() time.Time) *Reusable {
	if uses < 1 || ttl <= 0 {
		panic("turnstile: uses harus >= 1 dan ttl > 0")
	}
	return &Reusable{inner: inner, uses: uses, ttl: ttl, maxItems: 10_000, now: now, entries: make(map[[32]byte]*reuseEntry)}
}

func (r *Reusable) Verify(ctx context.Context, token, remoteIP string) error {
	key := sha256.Sum256([]byte(token))
	r.mu.Lock()
	r.sweepLocked()
	e, ok := r.entries[key]
	if !ok {
		if len(r.entries) >= r.maxItems {
			r.mu.Unlock()
			return r.inner.Verify(ctx, token, remoteIP) // penuh: tanpa cache (fail closed tetap berlaku)
		}
		e = &reuseEntry{done: make(chan struct{}), ip: remoteIP, left: r.uses - 1, expires: r.now().Add(r.ttl)}
		r.entries[key] = e
		r.mu.Unlock()
		e.err = r.inner.Verify(ctx, token, remoteIP)
		r.mu.Lock()
		if e.err != nil {
			delete(r.entries, key)
		}
		close(e.done)
		r.mu.Unlock()
		return e.err
	}
	r.mu.Unlock()

	select {
	case <-e.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if e.err != nil {
		return failed()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.ip != remoteIP || e.left <= 0 || !r.now().Before(e.expires) {
		return failed()
	}
	e.left--
	return nil
}

// sweepLocked membuang entri yang kedaluwarsa, paling sering sekali per detik. Dipanggil dengan lock.
func (r *Reusable) sweepLocked() {
	now := r.now()
	if now.Sub(r.lastSweep) < time.Second {
		return
	}
	r.lastSweep = now
	for k, e := range r.entries {
		select {
		case <-e.done:
			if !now.Before(e.expires) {
				delete(r.entries, k)
			}
		default: // masih diverifikasi
		}
	}
}
