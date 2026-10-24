// Package handler berisi handler HTTP tipis: bind request, panggil usecase, petakan hasil.
package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

const readyzTimeout = 2 * time.Second

// Pinger dipenuhi oleh *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Healthz adalah liveness probe: proses hidup dan bisa melayani request.
func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readyz adalah readiness probe: hanya memeriksa database (spec §5.6). Layanan lain
// (mis. wa-notif) sengaja tidak memengaruhi readiness; statusnya dipantau lewat alert.
func Readyz(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readyzTimeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			_ = c.Error(err) // detail error tidak dikirim ke klien
			httpx.Error(c, http.StatusServiceUnavailable, "NOT_READY", "database unavailable")
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
