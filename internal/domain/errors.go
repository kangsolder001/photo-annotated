// Package domain berisi entity dan aturan bisnis murni. Paket ini tidak melakukan I/O dan
// tidak mengenal HTTP/SQL; satu-satunya dependensi di luar stdlib adalah tipe UUID.
package domain

import (
	"errors"
	"fmt"
)

// Kind mengelompokkan error domain. Lapisan delivery memetakan Kind ke status HTTP di satu
// tempat; usecase dan domain tidak pernah menyebut kode HTTP.
type Kind int

const (
	KindInvalid      Kind = iota + 1 // input tidak memenuhi aturan (→ 422)
	KindNotFound                     // resource tidak ada atau bukan milik pemanggil (→ 404)
	KindUnauthorized                 // kredensial tidak ada/tidak valid (→ 401)
	KindForbidden                    // dikenali tetapi tidak boleh (→ 403)
	KindConflict                     // bentrok dengan keadaan saat ini (→ 409)
	KindGone                         // dulu ada/berlaku, kini kedaluwarsa (→ 410)
	KindUnavailable                  // layanan/penyimpanan sementara tidak tersedia (→ 503)
	KindRateLimited                  // melebihi batas pemakaian (→ 429)
)

// Error adalah error domain: Kind menentukan kelas, Code stabil untuk klien (mis.
// "INVALID_CONTENT_TYPE"), Message untuk manusia.
type Error struct {
	Kind    Kind
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Is membuat errors.Is(err, domain.ErrNotFound) cocok berdasarkan Code, bukan alamat pointer.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

func newErr(k Kind, code, format string, a ...any) *Error {
	return &Error{Kind: k, Code: code, Message: fmt.Sprintf(format, a...)}
}

func Invalid(code, format string, a ...any) *Error  { return newErr(KindInvalid, code, format, a...) }
func NotFound(code, format string, a ...any) *Error { return newErr(KindNotFound, code, format, a...) }
func Unauthorized(code, format string, a ...any) *Error {
	return newErr(KindUnauthorized, code, format, a...)
}
func Forbidden(code, format string, a ...any) *Error {
	return newErr(KindForbidden, code, format, a...)
}
func Conflict(code, format string, a ...any) *Error { return newErr(KindConflict, code, format, a...) }
func Gone(code, format string, a ...any) *Error     { return newErr(KindGone, code, format, a...) }
func Unavailable(code, format string, a ...any) *Error {
	return newErr(KindUnavailable, code, format, a...)
}
func RateLimited(code, format string, a ...any) *Error {
	return newErr(KindRateLimited, code, format, a...)
}

// Sentinel umum; pakai errors.Is untuk memeriksanya.
var (
	ErrNotFound = NotFound("NOT_FOUND", "resource not found")
)

// AsError mengambil *Error dari rantai error, jika ada.
func AsError(err error) (*Error, bool) {
	var de *Error
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}
