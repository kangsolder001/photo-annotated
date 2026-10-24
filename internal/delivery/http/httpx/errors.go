package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/domain"
)

// statusOf adalah satu-satunya tempat Kind error domain dipetakan ke status HTTP.
func statusOf(k domain.Kind) int {
	switch k {
	case domain.KindInvalid:
		return http.StatusUnprocessableEntity
	case domain.KindNotFound:
		return http.StatusNotFound
	case domain.KindUnauthorized:
		return http.StatusUnauthorized
	case domain.KindForbidden:
		return http.StatusForbidden
	case domain.KindConflict:
		return http.StatusConflict
	case domain.KindGone:
		return http.StatusGone
	case domain.KindUnavailable:
		return http.StatusServiceUnavailable
	case domain.KindRateLimited:
		return http.StatusTooManyRequests
	}
	return http.StatusInternalServerError
}

// WriteError menulis respons untuk error dari usecase. Error domain dipetakan ke status dan
// kode yang stabil; error lain menjadi 500 generik. Error asli (termasuk penyebab di balik
// error domain) dicatat di c.Errors untuk log dan tidak pernah dikirim ke klien.
func WriteError(c *gin.Context, err error) {
	_ = c.Error(err)
	if de, ok := domain.AsError(err); ok {
		AbortError(c, statusOf(de.Kind), de.Code, de.Message)
		return
	}
	AbortError(c, http.StatusInternalServerError, "INTERNAL", "internal server error")
}
