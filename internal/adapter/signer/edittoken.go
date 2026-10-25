package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/google/uuid"

	"photo-annotated/internal/usecase"
)

var _ usecase.EditTokenSigner = (*EditTokens)(nil)

// EditTokens menurunkan token edit secara stateless: HMAC-SHA256(EDIT_TOKEN_SECRET, "edit\n"+id).
// Tidak ada kolom token di DB, dan permintaan ulang idempoten menghasilkan token yang sama.
// Merotasi secret membatalkan semua token yang beredar (spec §9.3).
type EditTokens struct{ key []byte }

func NewEditTokens(secret string) (*EditTokens, error) {
	if len(secret) < minSecretLen {
		return nil, errors.New("signer: EDIT_TOKEN_SECRET minimal 32 karakter")
	}
	return &EditTokens{key: []byte(secret)}, nil
}

func (e *EditTokens) mac(id uuid.UUID) []byte {
	m := hmac.New(sha256.New, e.key)
	m.Write([]byte("edit\n" + id.String()))
	return m.Sum(nil)
}

func (e *EditTokens) Token(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(e.mac(id))
}

// Verify membandingkan secara constant-time.
func (e *EditTokens) Verify(id uuid.UUID, token string) bool {
	got, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(got) != sha256.Size {
		return false
	}
	return hmac.Equal(got, e.mac(id))
}
