// Package ratelimit berisi pembatas laju sliding-window log yang dipakai middleware HTTP (per IP)
// dan usecase (per nomor HP).
package ratelimit

import (
	"sync"
	"time"
)

const defaultMaxKeys = 100_000

// Limiter membatasi jumlah kejadian per kunci dengan sliding-window log: maksimal `limit` kejadian
// dalam `window` terakhir (di mana pun jendela itu jatuh), dan waktu tunggu dihitung tepat.
// State ada di memori proses (cukup untuk satu instance); hilang saat restart.
type Limiter struct {
	limit   int
	window  time.Duration
	maxKeys int
	now     func() time.Time

	mu        sync.Mutex
	events    map[string][]time.Time
	lastSweep time.Time
}

// New membuat limiter "limit kejadian per window per kunci".
func New(limit int, window time.Duration) *Limiter {
	return NewWithOptions(limit, window, defaultMaxKeys, time.Now)
}

// NewWithOptions seperti New dengan batas jumlah kunci dan jam yang dapat diatur (untuk test).
func NewWithOptions(limit int, window time.Duration, maxKeys int, now func() time.Time) *Limiter {
	if limit < 1 {
		panic("ratelimit: limit harus >= 1")
	}
	return &Limiter{limit: limit, window: window, maxKeys: maxKeys, now: now, events: make(map[string][]time.Time)}
}

// Allow mencatat satu request untuk key. Jika melebihi batas, mengembalikan false dan
// berapa lama harus menunggu.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= l.window {
		l.sweep(now)
	}

	ts := l.events[key]
	// buang catatan yang sudah keluar dari jendela
	cut := now.Add(-l.window)
	i := 0
	for i < len(ts) && !ts[i].After(cut) {
		i++
	}
	ts = ts[i:]

	if len(ts) >= l.limit {
		l.events[key] = ts
		return false, ts[0].Add(l.window).Sub(now)
	}
	if _, exists := l.events[key]; !exists && len(l.events) >= l.maxKeys {
		l.evictOne()
	}
	l.events[key] = append(ts, now)
	return true, 0
}

// sweep menghapus kunci yang seluruh catatannya sudah kedaluwarsa. Dipanggil dengan lock.
func (l *Limiter) sweep(now time.Time) {
	cut := now.Add(-l.window)
	for k, ts := range l.events {
		if len(ts) == 0 || !ts[len(ts)-1].After(cut) {
			delete(l.events, k)
		}
	}
	l.lastSweep = now
}

// evictOne membuang satu kunci sembarang agar jumlah kunci terbatas (memori tidak bisa
// dihabiskan dengan banyak IP). Konsekuensinya fail-open untuk kunci yang terbuang.
func (l *Limiter) evictOne() {
	for k := range l.events {
		delete(l.events, k)
		return
	}
}

// Len mengembalikan jumlah kunci yang sedang dilacak (untuk test/metrik).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

// Undo membuang kejadian terbaru milik key (mengembalikan satu jatah), mis. saat tindakan yang
// sudah dihitung ternyata tidak jadi dilakukan. Tidak berbuat apa-apa bila key tidak punya catatan.
func (l *Limiter) Undo(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ts := l.events[key]; len(ts) > 0 {
		if len(ts) == 1 {
			delete(l.events, key)
			return
		}
		l.events[key] = ts[:len(ts)-1]
	}
}
