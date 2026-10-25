package middleware

import (
	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/usecase"
)

// HeaderTurnstileToken membawa token Turnstile pada rute yang body-nya tidak memuat field token
// (presign dan submit pada auth.mode = none).
const HeaderTurnstileToken = "X-Turnstile-Token"

// Turnstile mewajibkan token Turnstile yang sah di header X-Turnstile-Token (gagal tertutup).
func Turnstile(v usecase.TurnstileVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := v.Verify(c.Request.Context(), c.GetHeader(HeaderTurnstileToken), httpx.ClientIP(c)); err != nil {
			httpx.WriteError(c, err)
			return
		}
		c.Next()
	}
}
