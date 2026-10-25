package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

var _ usecase.AdminTokenSigner = (*AdminTokens)(nil)

const adminTokenPrefix = "adm2."

// AdminTokens menerbitkan token admin stateless yang terikat ke nomor superadmin:
//
//	adm2.<exp unix>.<base64url(HMAC-SHA256(ADMIN_TOKEN_SECRET, "admin\n" ‖ phone ‖ "\n" ‖ exp))>
//
// Tidak ada state di server, jadi token tidak bisa dicabut satu per satu: mengganti nomor superadmin
// di config atau merotasi ADMIN_TOKEN_SECRET membatalkan semuanya (spec-labels-admin §3.3). Token
// adm1 lama (login username/password) tidak lagi dikenali.
type AdminTokens struct {
	key   []byte
	phone string
}

// NewAdminTokens: phone adalah nomor superadmin dalam bentuk kanonik.
func NewAdminTokens(secret, phone string) (*AdminTokens, error) {
	if len(secret) < minSecretLen {
		return nil, errors.New("signer: ADMIN_TOKEN_SECRET minimal 32 karakter")
	}
	if !domain.IsCanonicalPhone(phone) {
		return nil, errors.New("signer: nomor superadmin harus berbentuk kanonik 628…")
	}
	return &AdminTokens{key: []byte(secret), phone: phone}, nil
}

func (a *AdminTokens) mac(exp int64) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte("admin\n" + a.phone + "\n" + strconv.FormatInt(exp, 10)))
	return m.Sum(nil)
}

func (a *AdminTokens) Issue(expiresAt time.Time) string {
	exp := expiresAt.Unix()
	return adminTokenPrefix + strconv.FormatInt(exp, 10) + "." + base64.RawURLEncoding.EncodeToString(a.mac(exp))
}

// Verify memeriksa tanda tangan (constant-time) dan masa berlaku; mengembalikan waktu kedaluwarsa.
func (a *AdminTokens) Verify(token string, now time.Time) (time.Time, bool) {
	rest, ok := strings.CutPrefix(token, adminTokenPrefix)
	if !ok {
		return time.Time{}, false
	}
	expStr, sigStr, ok := strings.Cut(rest, ".")
	if !ok {
		return time.Time{}, false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || strconv.FormatInt(exp, 10) != expStr {
		return time.Time{}, false // tolak bentuk non-kanonik (mis. "+123", "0123")
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigStr)
	if err != nil || !hmac.Equal(sig, a.mac(exp)) {
		return time.Time{}, false
	}
	if now.Unix() >= exp {
		return time.Time{}, false
	}
	return time.Unix(exp, 0).UTC(), true
}
