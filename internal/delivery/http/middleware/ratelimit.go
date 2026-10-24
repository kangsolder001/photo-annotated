package middleware

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/ratelimit"
)

const (
	rateWindow = time.Minute
	unknownKey = "unknown"
)

// NewRateLimiter membuat limiter "perMinute request per menit per IP".
func NewRateLimiter(perMinute int) *ratelimit.Limiter { return ratelimit.New(perMinute, rateWindow) }

// RateLimit memasang limiter pada rute; kunci = IP klien (IPv6 dikelompokkan per /64).
func RateLimit(l *ratelimit.Limiter) gin.HandlerFunc {
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
