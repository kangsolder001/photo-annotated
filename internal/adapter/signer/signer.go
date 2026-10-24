// Package signer menandatangani URL upload/lihat foto dengan HMAC-SHA256.
package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/usecase"
)

const minSecretLen = 32

var _ usecase.URLSigner = (*Signer)(nil)

type Signer struct{ key []byte }

// New membuat Signer. Kunci minimal 32 byte (config sudah memvalidasi; ini pagar kedua).
func New(secret string) (*Signer, error) {
	if len(secret) < minSecretLen {
		return nil, errors.New("signer: secret minimal 32 karakter")
	}
	return &Signer{key: []byte(secret)}, nil
}

// message membentuk pesan yang ditandatangani. Awalan tujuan ("upload") memisahkan domain
// tanda tangan; field dipisah newline, dan tak satu pun field dapat memuat newline
// (UUID, angka, atau content-type yang sudah divalidasi), sehingga tidak ambigu.
func message(purpose string, photoID uuid.UUID, exp int64, contentType string, size int64) []byte {
	return []byte(purpose + "\n" + photoID.String() + "\n" + strconv.FormatInt(exp, 10) +
		"\n" + contentType + "\n" + strconv.FormatInt(size, 10))
}

func (s *Signer) mac(msg []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write(msg)
	return m.Sum(nil)
}

// SignUpload menghasilkan tanda tangan base64url untuk PUT /uploads/{photo_id}.
func (s *Signer) SignUpload(photoID uuid.UUID, exp time.Time, contentType string, sizeBytes int64) string {
	return base64.RawURLEncoding.EncodeToString(s.mac(message("upload", photoID, exp.Unix(), contentType, sizeBytes)))
}

// VerifyUpload memeriksa tanda tangan (constant-time). Kedaluwarsa diperiksa pemanggil
// setelah tanda tangan terbukti sah, supaya "kedaluwarsa" tidak bisa dipakai menebak tanda tangan.
func (s *Signer) VerifyUpload(photoID uuid.UUID, exp int64, contentType string, sizeBytes int64, sig string) bool {
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || len(got) != sha256.Size {
		return false
	}
	return hmac.Equal(got, s.mac(message("upload", photoID, exp, contentType, sizeBytes)))
}
