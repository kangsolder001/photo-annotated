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

	Presigner     handler.Presigner // usecase presign (spec §5.1)
	PresignPerMin int               // batas rate limit presign per IP

	Submitter         handler.Submitter // usecase submit (spec §5.2)
	SubmissionsPerMin int               // batas rate limit POST /submissions per IP

	Reader handler.Reader // usecase baca submission & foto (spec §5.3–5.5)

	Uploader       handler.Uploader // usecase upload (spec §5.1b)
	MaxUploadBytes int64            // batas ukuran file (config limits.max_upload_bytes)
	UploadTimeout  time.Duration    // deadline baca total upload (config limits.upload_timeout)
}

// NewRouter membuat engine dengan middleware dasar dan route operasional.
func NewRouter(d Deps) *gin.Engine {
	if d.Logger == nil || d.DB == nil || d.Presigner == nil || d.PresignPerMin < 1 ||
		d.Uploader == nil || d.MaxUploadBytes < 1 || d.UploadTimeout <= 0 ||
		d.Submitter == nil || d.SubmissionsPerMin < 1 || d.Reader == nil {
		panic("httpapi: Deps.Logger, DB, Presigner, PresignPerMin, Uploader, MaxUploadBytes, UploadTimeout, Submitter, SubmissionsPerMin, dan Reader wajib diisi")
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

	api := ops.Group("/api/v1")
	uploads := handler.NewUploadHandler(d.Presigner, d.Uploader, d.MaxUploadBytes)
	api.POST("/uploads/presign", middleware.RateLimit(middleware.NewRateLimiter(d.PresignPerMin)), uploads.Presign)

	submissions := handler.NewSubmissionHandler(d.Submitter)
	api.POST("/submissions", middleware.RateLimit(middleware.NewRateLimiter(d.SubmissionsPerMin)), submissions.Create)

	reads := handler.NewReadHandler(d.Reader)
	api.GET("/submissions/:id", reads.GetSubmission)
	api.GET("/photos/:id/url", reads.PhotoURL)

	// Konten foto: di luar grup JSON karena deadline tulisnya harus lebih panjang (klien lambat).
	r.GET("/api/v1/photos/:id/content", middleware.Deadlines(0, d.UploadTimeout), reads.PhotoContent)

	// Upload foto: di luar grup JSON (tanpa batas body 64 KB dan tanpa deadline baca 10 detik).
	// Batas ukuran ditegakkan usecase (tepat sebesar yang dideklarasikan); deadline baca total
	// mengikuti limits.upload_timeout supaya klien lambat tidak menahan slot selamanya.
	r.PUT("/api/v1/uploads/:photo_id", middleware.Deadlines(d.UploadTimeout, jsonWriteDeadline), uploads.Put)

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
