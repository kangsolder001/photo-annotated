package signer_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/signer"
)

const pepper = "pepper-0123456789abcdef0123456789abcdef"

func TestOTPCodesShapeAndDeterminism(t *testing.T) {
	for _, n := range []int{4, 6, 8} {
		c, err := signer.NewOTPCodes(pepper, n)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for i := 0; i < 500; i++ {
			id := uuid.New()
			code := c.Code(id)
			if len(code) != n {
				t.Fatalf("len(%q) = %d, ingin %d", code, len(code), n)
			}
			for _, r := range code {
				if r < '0' || r > '9' {
					t.Fatalf("kode %q bukan digit", code)
				}
			}
			if c.Code(id) != code {
				t.Fatal("kode harus deterministik untuk id yang sama")
			}
			seen[code] = true
		}
		if n >= 6 && len(seen) < 490 {
			t.Fatalf("kode terlalu sering sama: %d unik dari 500", len(seen))
		}
	}
}

func TestOTPCodesDependOnPepper(t *testing.T) {
	a, _ := signer.NewOTPCodes(pepper, 6)
	b, _ := signer.NewOTPCodes(pepper+"x", 6)
	same := 0
	for i := 0; i < 50; i++ {
		id := uuid.New()
		if a.Code(id) == b.Code(id) {
			same++
		}
	}
	if same > 2 {
		t.Fatalf("pepper berbeda menghasilkan %d kode sama dari 50", same)
	}
}

func TestOTPCodesHash(t *testing.T) {
	c, _ := signer.NewOTPCodes(pepper, 6)
	id := uuid.New()
	h := c.Hash(id, c.Code(id))
	if len(h) != 32 {
		t.Fatalf("hash %d byte, kolom code_hash butuh 32", len(h))
	}
	if !bytes.Equal(h, c.Hash(id, c.Code(id))) {
		t.Fatal("hash harus deterministik")
	}
	if bytes.Equal(h, c.Hash(id, "000000")) && c.Code(id) != "000000" {
		t.Fatal("kode berbeda menghasilkan hash sama")
	}
	if bytes.Equal(h, c.Hash(uuid.New(), c.Code(id))) {
		t.Fatal("hash harus terikat ke challenge id")
	}
}

func TestNewOTPCodesRejectsBadInput(t *testing.T) {
	if _, err := signer.NewOTPCodes("pendek", 6); err == nil {
		t.Fatal("pepper pendek harus ditolak")
	}
	for _, n := range []int{0, 3, 9} {
		if _, err := signer.NewOTPCodes(pepper, n); err == nil {
			t.Fatalf("panjang %d harus ditolak", n)
		}
	}
}
