// Package password menyediakan hash password bcrypt.
package password

import (
	"golang.org/x/crypto/bcrypt"

	"photo-annotated/internal/usecase"
)

// DefaultCost: ±60–80 ms per hash di Raspberry Pi 4, cukup mahal untuk brute force offline namun
// murah untuk login biasa (login juga dibatasi per IP dan per nomor).
const DefaultCost = 10

type Bcrypt struct {
	cost  int
	dummy []byte // dipakai saat akun tidak ada/tanpa password, agar waktu respons sama
}

var _ usecase.PasswordHasher = (*Bcrypt)(nil)

func NewBcrypt(cost int) (*Bcrypt, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), cost)
	if err != nil {
		return nil, err
	}
	return &Bcrypt{cost: cost, dummy: dummy}, nil
}

func (b *Bcrypt) Hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), b.cost)
	return string(h), err
}

func (b *Bcrypt) Compare(hash, password string) bool {
	h := []byte(hash)
	if len(h) == 0 {
		_ = bcrypt.CompareHashAndPassword(b.dummy, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword(h, []byte(password)) == nil
}
