// Package httpapi merakit engine Gin: middleware, route, dan server HTTP.
package httpapi

import (
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/handler"
	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/delivery/http/middleware"
)

const (
	// Deadline default rute JSON (spec §11.3).
	jsonReadDeadline  = 10 * time.Second
	jsonWriteDeadline = 15 * time.Second
)

// Deps adalah semua yang dibutuhkan router; diisi di composition root (cmd/api).
type Deps struct {
	Logger         *slog.Logger
	TrustedProxies []netip.Prefix
}

// NewRouter membuat engine dengan middleware dasar dan route operasional.
func NewRouter(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// IP klien ditentukan middleware sendiri; jangan biarkan gin mempercayai header apa pun.
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err) // nil selalu valid; panic menandakan bug
	}
	r.HandleMethodNotAllowed = true
	r.NoRoute(httpx.NotFound)
	r.NoMethod(httpx.MethodNotAllowed)

	r.Use(
		middleware.RequestID(),
		middleware.ClientIP(d.TrustedProxies),
		middleware.Logger(d.Logger),
		middleware.Recovery(d.Logger),
	)

	ops := r.Group("", middleware.Deadlines(jsonReadDeadline, jsonWriteDeadline))
	ops.GET("/healthz", handler.Healthz)

	return r
}

// NewServer membuat http.Server tanpa ReadTimeout/WriteTimeout global (lihat middleware.Deadlines).
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
