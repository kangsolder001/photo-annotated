package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"photo-annotated/internal/usecase"
)

var _ usecase.OTPCodes = (*OTPCodes)(nil)

// OTPCodes menurunkan kode OTP = HMAC-SHA256(OTP_PEPPER, "otp-code\n"+id) mod 10^length. Challenge ID
// adalah UUID v4 acak (crypto/rand) dan HMAC adalah PRF, jadi kode tak dapat ditebak tanpa pepper,
// tanpa perlu menyimpan kode asli atau mengenkripsinya untuk dispatcher. Bias modulo dari 64 bit ke
// ≤ 10^8 dapat diabaikan (< 2^-37).
type OTPCodes struct {
	key    []byte
	length int
	mod    uint64
}

func NewOTPCodes(pepper string, length int) (*OTPCodes, error) {
	if len(pepper) < minSecretLen {
		return nil, errors.New("signer: OTP_PEPPER minimal 32 karakter")
	}
	if length < 4 || length > 8 {
		return nil, errors.New("signer: panjang OTP harus 4-8")
	}
	mod := uint64(1)
	for range length {
		mod *= 10
	}
	return &OTPCodes{key: []byte(pepper), length: length, mod: mod}, nil
}

func (o *OTPCodes) mac(msg string) []byte {
	m := hmac.New(sha256.New, o.key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func (o *OTPCodes) Code(id uuid.UUID) string {
	n := binary.BigEndian.Uint64(o.mac("otp-code\n" + id.String())[:8])
	return fmt.Sprintf("%0*d", o.length, n%o.mod)
}

// Hash adalah nilai kolom code_hash: HMAC-SHA256(OTP_PEPPER, "otp-hash\n"+id+"\n"+code).
func (o *OTPCodes) Hash(id uuid.UUID, code string) []byte {
	return o.mac("otp-hash\n" + id.String() + "\n" + code)
}
