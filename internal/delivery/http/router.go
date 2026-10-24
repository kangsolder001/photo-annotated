// Package httpapi merakit engine Gin: middleware, route, dan server HTTP.
package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/handler"
	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/delivery/http/middleware"
	"photo-annotated/internal/usecase"
)

const (
	// Deadline default rute JSON (spec §11.3).
	jsonReadDeadline  = 10 * time.Second
	jsonWriteDeadline = 15 * time.Second
	// export: batas waktu menulis satu halaman ke klien (deadline digeser tiap halaman)
	exportPageGrace = 60 * time.Second
	// hapus massal: semua file dihapus sebelum respons ditulis
	bulkDeleteDeadline = 5 * time.Minute
)

// AdminAuth dipenuhi oleh *usecase.AdminLogin: login superadmin lewat OTP dan verifikasi token admin.
type AdminAuth interface {
	handler.AdminLoginService
	middleware.AdminVerifier
}

// SessionManager dipenuhi oleh *usecase.Sessions: validasi token bearer, profil, dan logout.
type SessionManager interface {
	handler.SessionService
	middleware.SessionAuthenticator
}

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
	// Label terkontrol (spec-labels-admin §4); nil = rute /labels dan /admin/labels tidak dipasang.
	Labels handler.LabelService
	Editor handler.Editor // usecase edit submission (spec §5.4)

	// Auth OTP (spec §5.8); nil bila auth.mode = none sehingga rute /auth/* menjadi 404.
	OTPRequester     handler.OTPRequester
	OTPChannel       handler.ChannelStatus
	Turnstile        usecase.TurnstileVerifier // nil bila turnstile.enabled = false
	OTPRequestPerMin int                       // batas register + otp/request per IP
	OTPVerifier      handler.OTPVerifier
	Sessions         SessionManager
	OTPVerifyPerMin  int // batas otp/verify per IP
	// Login nomor HP + password (mode otp)
	PasswordLogin handler.PasswordLoginer
	LoginPerMin   int

	// Turnstile untuk presign dan submit pada auth.mode = none (token boleh dipakai ulang beberapa
	// kali untuk satu submission); nil = tanpa Turnstile.
	UploadTurnstile usecase.TurnstileVerifier

	// Superadmin (spec-labels-admin §3); nil bila admin.enabled = false sehingga /admin/* menjadi 404.
	// Login admin memakai OTPChannel, Turnstile, dan OTPVerifyPerMin yang sama dengan auth pengguna.
	Admin            AdminAuth
	AdminLoginPerMin int    // request + resend OTP admin per IP
	AuthMode         string // auth.mode, untuk GET /admin/me

	// Hasil anotasi untuk dashboard (spec-labels-admin §5.4); hanya aktif bila Admin juga diisi.
	AdminSubmissions handler.AdminSubmissionReader

	// Hapus permanen oleh admin (spec-labels-admin §5.5); hanya aktif bila Admin juga diisi.
	AdminDelete handler.AdminDeleter

	// Export dataset (spec §5.7); hanya aktif bila Admin juga diisi.
	Exporter handler.Exporter
	NewJSONL func(io.Writer) handler.ExportStream

	Uploader       handler.Uploader // usecase upload (spec §5.1b)
	MaxUploadBytes int64            // batas ukuran file (config limits.max_upload_bytes)
	UploadTimeout  time.Duration    // deadline baca total upload (config limits.upload_timeout)
}

// NewRouter membuat engine dengan middleware dasar dan route operasional.
func NewRouter(d Deps) *gin.Engine {
	if d.Logger == nil || d.DB == nil || d.Presigner == nil || d.PresignPerMin < 1 ||
		d.Uploader == nil || d.MaxUploadBytes < 1 || d.UploadTimeout <= 0 ||
		d.Submitter == nil || d.SubmissionsPerMin < 1 || d.Reader == nil || d.Editor == nil {
		panic("httpapi: Deps.Logger, DB, Presigner, PresignPerMin, Uploader, MaxUploadBytes, UploadTimeout, Submitter, SubmissionsPerMin, Reader, dan Editor wajib diisi")
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
	// Penjaga rute (spec §9.2). Mode otp (Sessions terisi): sesi wajib untuk menulis; mode none:
	// anonim (kepemilikan lewat edit_token) dengan Turnstile pada presign dan submit. Token admin
	// hanya boleh membaca.
	var sessions middleware.SessionAuthenticator
	if d.Sessions != nil {
		sessions = d.Sessions
	}
	var adminTokens middleware.AdminVerifier
	if d.Admin != nil {
		adminTokens = d.Admin
	}
	authn := middleware.NewAuthenticator(sessions, adminTokens)
	otpMode := sessions != nil
	write := []gin.HandlerFunc{authn.Require(middleware.Access{Anonymous: !otpMode, User: otpMode})}
	read := []gin.HandlerFunc{authn.Require(middleware.Access{Anonymous: !otpMode, User: otpMode, Admin: true})}
	var human []gin.HandlerFunc
	if !otpMode && d.UploadTurnstile != nil {
		human = []gin.HandlerFunc{middleware.Turnstile(d.UploadTurnstile)}
	}
	chain := func(hs ...[]gin.HandlerFunc) []gin.HandlerFunc {
		var out []gin.HandlerFunc
		for _, h := range hs {
			out = append(out, h...)
		}
		return out
	}

	uploads := handler.NewUploadHandler(d.Presigner, d.Uploader, d.MaxUploadBytes)
	presignLimit := []gin.HandlerFunc{middleware.RateLimit(middleware.NewRateLimiter(d.PresignPerMin))}
	api.POST("/uploads/presign", chain(presignLimit, write, human, []gin.HandlerFunc{uploads.Presign})...)

	submissions := handler.NewSubmissionHandler(d.Submitter)
	edits := handler.NewEditHandler(d.Editor)
	// POST dan PUT berbagi satu limiter per IP ("submissions (POST & PUT): 10 / menit", spec §9.4).
	submissionsLimit := []gin.HandlerFunc{middleware.RateLimit(middleware.NewRateLimiter(d.SubmissionsPerMin))}
	api.POST("/submissions", chain(submissionsLimit, write, human, []gin.HandlerFunc{submissions.Create})...)
	api.PUT("/submissions/:id", chain(submissionsLimit, write, []gin.HandlerFunc{edits.Put})...)

	reads := handler.NewReadHandler(d.Reader)
	api.GET("/submissions/:id", chain(read, []gin.HandlerFunc{reads.GetSubmission})...)
	api.GET("/photos/:id/url", chain(read, []gin.HandlerFunc{reads.PhotoURL})...)

	var labels *handler.LabelHandler
	if d.Labels != nil {
		labels = handler.NewLabelHandler(d.Labels, d.Logger)
		// akses sama dengan presign (tanpa Turnstile karena hanya baca); token admin juga boleh
		api.GET("/labels", chain(read, []gin.HandlerFunc{labels.Active})...)
	}

	if d.OTPRequester != nil {
		if d.OTPRequestPerMin < 1 || d.OTPChannel == nil || d.OTPVerifier == nil || d.Sessions == nil || d.OTPVerifyPerMin < 1 ||
			d.PasswordLogin == nil || d.LoginPerMin < 1 {
			panic("httpapi: Deps.OTPRequestPerMin, OTPChannel, OTPVerifier, Sessions, OTPVerifyPerMin, PasswordLogin, dan LoginPerMin wajib diisi bila OTPRequester diisi")
		}
		auth := handler.NewAuthHandler(handler.AuthDeps{
			OTP: d.OTPRequester, Verifier: d.OTPVerifier, Login: d.PasswordLogin, Sessions: d.Sessions, Channel: d.OTPChannel, Turnstile: d.Turnstile,
		})
		// register, password/forgot, dan otp/resend sama-sama memicu pesan WhatsApp: satu limiter per IP.
		otpLimit := middleware.RateLimit(middleware.NewRateLimiter(d.OTPRequestPerMin))
		api.POST("/auth/register", otpLimit, auth.Register)
		api.POST("/auth/password/forgot", otpLimit, auth.Forgot)
		api.POST("/auth/otp/resend", otpLimit, auth.Resend)
		api.POST("/auth/login", middleware.RateLimit(middleware.NewRateLimiter(d.LoginPerMin)), auth.Login)
		api.GET("/auth/otp/:id", auth.OTPStatus)
		api.GET("/auth/channel-status", auth.ChannelStatus)
		api.POST("/auth/otp/verify", middleware.RateLimit(middleware.NewRateLimiter(d.OTPVerifyPerMin)), auth.Verify)
		// Rute auth publik di atas sengaja tanpa pemeriksaan sesi: token basi di FE tidak boleh
		// menghalangi login ulang.
		session := authn.Require(middleware.Access{User: true})
		api.GET("/auth/me", session, auth.Me)
		api.POST("/auth/logout", session, auth.Logout)
	}

	if d.Admin != nil {
		if d.AdminLoginPerMin < 1 || d.OTPVerifyPerMin < 1 || d.OTPChannel == nil {
			panic("httpapi: Deps.AdminLoginPerMin, OTPVerifyPerMin, dan OTPChannel wajib diisi bila Admin diisi")
		}
		adminAuth := handler.NewAdminAuthHandler(handler.AdminAuthDeps{
			Login: d.Admin, Channel: d.OTPChannel, Turnstile: d.Turnstile, AuthMode: d.AuthMode, Log: d.Logger,
		})
		// request dan resend sama-sama memicu pesan WhatsApp: satu limiter per IP (admin_login).
		adminOTPLimit := middleware.RateLimit(middleware.NewRateLimiter(d.AdminLoginPerMin))
		api.POST("/admin/otp/request", adminOTPLimit, adminAuth.Request)
		api.POST("/admin/otp/resend", adminOTPLimit, adminAuth.Resend)
		api.GET("/admin/otp/:id", adminAuth.Status)
		api.POST("/admin/otp/verify", middleware.RateLimit(middleware.NewRateLimiter(d.OTPVerifyPerMin)), adminAuth.Verify)
		api.GET("/admin/channel-status", adminAuth.ChannelStatus)
		adminOnly := authn.Require(middleware.Access{Admin: true})
		api.GET("/admin/me", adminOnly, adminAuth.Me)
		if labels != nil {
			api.GET("/admin/labels", adminOnly, labels.AdminList)
			api.POST("/admin/labels", adminOnly, labels.Create)
			api.PUT("/admin/labels/order", adminOnly, labels.Reorder)
			api.PUT("/admin/labels/:id", adminOnly, labels.Update)
			api.DELETE("/admin/labels/:id", adminOnly, labels.Delete)
		}
		if d.AdminSubmissions != nil {
			subsAdmin := handler.NewAdminSubmissionHandler(d.AdminSubmissions, d.Logger)
			api.GET("/admin/submissions", adminOnly, subsAdmin.List)
			api.GET("/admin/submissions/:id", adminOnly, subsAdmin.Detail)
			api.PUT("/admin/submissions/:id/review", adminOnly, subsAdmin.Review)
			api.POST("/admin/submissions/review", adminOnly, subsAdmin.ReviewMany)
		}
		if d.AdminDelete != nil {
			dels := handler.NewAdminDeleteHandler(d.AdminDelete, d.Logger)
			api.DELETE("/admin/submissions/:id", adminOnly, dels.DeleteSubmission)
			api.DELETE("/admin/photos/:id", adminOnly, dels.DeletePhoto)
			// Hapus massal (50 submission × 5 foto = 250 file): deadline tulis diperpanjang seperti export.
			r.POST("/api/v1/admin/submissions/delete", middleware.Deadlines(jsonReadDeadline, bulkDeleteDeadline),
				middleware.BodyLimit(middleware.JSONBodyLimit), adminOnly, dels.DeleteMany)
		}
	}

	if d.Admin != nil && d.Exporter != nil {
		// Di luar grup JSON: deadline tulis awal 60 detik, diperpanjang setiap halaman oleh handler.
		exports := handler.NewExportHandler(d.Exporter, d.NewJSONL, exportPageGrace)
		r.GET("/api/v1/admin/export", middleware.Deadlines(jsonReadDeadline, exportPageGrace),
			authn.Require(middleware.Access{Admin: true}), exports.Export)
	}

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
