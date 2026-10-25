package httpx

import (
	"math"
	"net/http"
	"strconv"

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
	case domain.KindLengthRequired:
		return http.StatusLengthRequired
	case domain.KindTimeout:
		return http.StatusRequestTimeout
	}
	return http.StatusInternalServerError
}

// WriteError menulis respons untuk error dari usecase. Error domain dipetakan ke status dan
// kode yang stabil; error lain menjadi 500 generik. Error asli (termasuk penyebab di balik
// error domain) dicatat di c.Errors untuk log dan tidak pernah dikirim ke klien.
func WriteError(c *gin.Context, err error) {
	_ = c.Error(err)
	if de, ok := domain.AsError(err); ok {
		if de.RetryAfter > 0 {
			secs := int(math.Ceil(de.RetryAfter.Seconds()))
			c.Header("Retry-After", strconv.Itoa(max(secs, 1)))
		}
		c.Abort()
		c.JSON(statusOf(de.Kind), errorBody{Error: errorDetail{Code: de.Code, Message: de.Message, Details: de.Details}})
		return
	}
	AbortError(c, http.StatusInternalServerError, "INTERNAL", "internal server error")
}
