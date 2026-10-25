package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Deadlines memasang deadline baca/tulis per-route lewat http.ResponseController.
// Server sengaja tidak memakai ReadTimeout/WriteTimeout global (spec §11.3): ReadTimeout
// global akan memutus upload dari koneksi HP yang lambat. Nilai 0 berarti "tidak diubah".
// Writer yang tidak mendukung deadline (mis. httptest.ResponseRecorder) diabaikan.
func Deadlines(read, write time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		rc := http.NewResponseController(c.Writer)
		now := time.Now()
		if read > 0 {
			if err := rc.SetReadDeadline(now.Add(read)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				c.Error(err) //nolint:errcheck // dicatat di c.Errors, tidak menghentikan request
			}
		}
		if write > 0 {
			if err := rc.SetWriteDeadline(now.Add(write)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				c.Error(err) //nolint:errcheck
			}
		}
		c.Next()
	}
}
