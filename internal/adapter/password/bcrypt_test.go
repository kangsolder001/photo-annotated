package password_test

import (
	"testing"

	"golang.org/x/crypto/bcrypt"

	"photo-annotated/internal/adapter/password"
)

func TestBcrypt(t *testing.T) {
	b, err := password.NewBcrypt(bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	h, err := b.Hash("rahasia123")
	if err != nil || len(h) != 60 {
		t.Fatalf("hash %q %v", h, err)
	}
	if !b.Compare(h, "rahasia123") || b.Compare(h, "rahasia124") || b.Compare("", "rahasia123") || b.Compare("bukan-hash", "x") {
		t.Fatal("perbandingan salah")
	}
	h2, _ := b.Hash("rahasia123")
	if h == h2 {
		t.Fatal("hash harus memakai salt acak")
	}
}
