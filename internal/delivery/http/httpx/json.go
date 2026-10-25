package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

// DecodeJSON membaca body JSON ke dst secara ketat (field tak dikenal ditolak, tidak boleh
// ada data setelah objek). Jika gagal, respons error sudah ditulis dan false dikembalikan.
func DecodeJSON(c *gin.Context, dst any) bool {
	if mt, _, err := mime.ParseMediaType(c.GetHeader("Content-Type")); err != nil || mt != "application/json" {
		AbortError(c, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return false
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeDecodeError(c, err)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			AbortError(c, http.StatusBadRequest, "INVALID_JSON", "unexpected data after JSON body")
		} else {
			writeDecodeError(c, err)
		}
		return false
	}
	return true
}

func writeDecodeError(c *gin.Context, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		AbortError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body too large")
		return
	}
	AbortError(c, http.StatusBadRequest, "INVALID_JSON", "request body is not valid JSON for this endpoint")
}
