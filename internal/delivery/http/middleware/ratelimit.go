package middleware

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

const (
	rateWindow     = time.Minute
	defaultMaxKeys = 100_000
	unknownKey     = "unknown"
)

// RateLimiter membatasi jumlah request per kunci (IP klien) dengan sliding-window log:
// maksimal `limit` request dalam rentang 60 detik terakhir, dan Retry-After tepat.
// State ada di memori proses (cukup untuk satu instance); hilang saat restart.
type RateLimiter struct {
	limit   int
	window  time.Duration
	maxKeys int
	now     func() time.Time

	mu        sync.Mutex
	events    map[string][]time.Time
	lastSweep time.Time
}

// NewRateLimiter membuat limiter "perMinute request per menit per IP".
func NewRateLimiter(perMinute int) *RateLimiter {
	return newRateLimiter(perMinute, rateWindow, defaultMaxKeys, time.Now)
}

func newRateLimiter(limit int, window time.Duration, maxKeys int, now func() time.Time) *RateLimiter {
	if limit < 1 {
		panic("middleware: limit rate harus >= 1")
	}
	return &RateLimiter{limit: limit, window: window, maxKeys: maxKeys, now: now, events: make(map[string][]time.Time)}
}

// Allow mencatat satu request untuk key. Jika melebihi batas, mengembalikan false dan
// berapa lama harus menunggu.
func (l *RateLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
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
func (l *RateLimiter) sweep(now time.Time) {
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
func (l *RateLimiter) evictOne() {
	for k := range l.events {
		delete(l.events, k)
		return
	}
}

// Len mengembalikan jumlah kunci yang sedang dilacak (untuk test/metrik).
func (l *RateLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

// RateLimit memasang limiter pada rute; kunci = IP klien (IPv6 dikelompokkan per /64).
func RateLimit(l *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, retry := l.Allow(rateKey(httpx.ClientIP(c)))
		if ok {
			c.Next()
			return
		}
		secs := int(math.Ceil(retry.Seconds()))
		if secs < 1 {
			secs = 1
		}
		c.Header("Retry-After", strconv.Itoa(secs))
		httpx.AbortError(c, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, retry later")
	}
}

func rateKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return unknownKey
	}
	a = a.Unmap()
	if a.Is6() {
		// satu pengguna IPv6 biasanya memegang seluruh /64; kelompokkan agar tidak mudah diakali
		return netip.PrefixFrom(a, 64).Masked().String()
	}
	return a.String()
}
