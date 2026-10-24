package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

// Recovery mengubah panic menjadi 500 berformat error; proses tetap hidup.
// Stack trace hanya masuk log, tidak pernah ke respons.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel khusus net/http
					panic(rec)
				}
				log.LogAttrs(c.Request.Context(), slog.LevelError, "panic",
					slog.String("request_id", httpx.RequestID(c)),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				if !c.Writer.Written() {
					httpx.AbortError(c, http.StatusInternalServerError, "INTERNAL", "internal server error")
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}
