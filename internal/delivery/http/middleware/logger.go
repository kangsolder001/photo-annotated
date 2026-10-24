package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
)

// Logger mencatat satu baris JSON per request dengan field minimum spec §12 (user_id bila ada sesi).
// Sengaja TIDAK mencatat query string (memuat tanda tangan URL), header
// (Authorization, X-Edit-Token), maupun body.
func Logger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		case c.FullPath() == "/healthz" || c.FullPath() == "/readyz":
			level = slog.LevelDebug // probe Docker/monitor tiap 30 detik: jangan membanjiri log
		}
		attrs := []slog.Attr{
			slog.String("request_id", httpx.RequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.String("route", c.FullPath()),
			slog.Int("status", status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("client_ip", httpx.ClientIP(c)),
			slog.Int("bytes", c.Writer.Size()),
		}
		if userID, _, ok := httpx.Session(c); ok {
			attrs = append(attrs, slog.String("user_id", userID.String()))
		}
		if httpx.IsAdmin(c) {
			attrs = append(attrs, slog.Bool("admin", true))
		}
		// Error internal yang dicatat handler lewat c.Error(err): hanya masuk log, tidak ke klien.
		// Kode error domain dicatat terpisah agar mudah dihitung di dashboard (mis. UPLOAD_BUSY).
		if errs := c.Errors.ByType(gin.ErrorTypePrivate); len(errs) > 0 {
			attrs = append(attrs, slog.String("error", errs.String()))
			if de, ok := domain.AsError(errs.Last().Err); ok {
				attrs = append(attrs, slog.String("code", de.Code))
			}
		}
		log.LogAttrs(c.Request.Context(), level, "request", attrs...)
	}
}
