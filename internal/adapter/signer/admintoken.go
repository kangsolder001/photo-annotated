package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"photo-annotated/internal/usecase"
)

var _ usecase.AdminTokenSigner = (*AdminTokens)(nil)

const adminTokenPrefix = "adm1."

// AdminTokens menerbitkan token admin stateless: "adm1.<exp unix>.<base64url(HMAC-SHA256(
// ADMIN_TOKEN_SECRET, "admin\n"+exp))>". Tidak ada state di server, jadi token tidak bisa dicabut
// satu per satu: merotasi ADMIN_TOKEN_SECRET membatalkan semuanya (spec §9.1).
type AdminTokens struct{ key []byte }

func NewAdminTokens(secret string) (*AdminTokens, error) {
	if len(secret) < minSecretLen {
		return nil, errors.New("signer: ADMIN_TOKEN_SECRET minimal 32 karakter")
	}
	return &AdminTokens{key: []byte(secret)}, nil
}

func (a *AdminTokens) mac(exp int64) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte("admin\n" + strconv.FormatInt(exp, 10)))
	return m.Sum(nil)
}

func (a *AdminTokens) Issue(expiresAt time.Time) string {
	exp := expiresAt.Unix()
	return adminTokenPrefix + strconv.FormatInt(exp, 10) + "." + base64.RawURLEncoding.EncodeToString(a.mac(exp))
}

// Verify memeriksa tanda tangan (constant-time) dan masa berlaku.
func (a *AdminTokens) Verify(token string, now time.Time) bool {
	rest, ok := strings.CutPrefix(token, adminTokenPrefix)
	if !ok {
		return false
	}
	expStr, sigStr, ok := strings.Cut(rest, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || strconv.FormatInt(exp, 10) != expStr {
		return false // tolak bentuk non-kanonik (mis. "+123", "0123")
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigStr)
	if err != nil || !hmac.Equal(sig, a.mac(exp)) {
		return false
	}
	return now.Unix() < exp
}
