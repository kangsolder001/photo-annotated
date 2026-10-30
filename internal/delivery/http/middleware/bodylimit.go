package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

// JSONBodyLimit adalah batas body request JSON (spec §9.4). Rute upload foto tidak memakainya.
const JSONBodyLimit int64 = 128 << 10 // 128 KB: pendataan memuat jawaban + keterangan (spec-pendataan §5.3)

// BodyLimit menolak body lebih besar dari max: lebih dulu lewat Content-Length (413 cepat,
// tanpa membaca body), dan untuk body chunked lewat http.MaxBytesReader saat dibaca.
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > max {
			httpx.AbortError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body too large")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		c.Next()
	}
}
