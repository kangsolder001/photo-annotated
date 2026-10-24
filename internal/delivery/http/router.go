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
	DB             handler.Pinger // untuk /readyz
	AllowedOrigins []string       // origin FE yang diizinkan CORS
}

// NewRouter membuat engine dengan middleware dasar dan route operasional.
func NewRouter(d Deps) *gin.Engine {
	if d.Logger == nil || d.DB == nil {
		panic("httpapi: Deps.Logger dan Deps.DB wajib diisi")
	}
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
		// CORS global & sebelum routing: preflight dijawab tanpa auth/rate limit, untuk rute apa pun.
		middleware.CORS(d.AllowedOrigins),
	)

	// Rute JSON: deadline 10s/15s dan body maks 64 KB. Rute upload foto (T12) sengaja tidak
	// memakai grup ini: ia punya deadline dan batas body sendiri.
	ops := r.Group("", middleware.Deadlines(jsonReadDeadline, jsonWriteDeadline), middleware.BodyLimit(middleware.JSONBodyLimit))
	ops.GET("/healthz", handler.Healthz)
	ops.GET("/readyz", handler.Readyz(d.DB))

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
