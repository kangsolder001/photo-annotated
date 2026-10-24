package signer_test

import (
	"strings"
	"testing"
	"time"

	"photo-annotated/internal/adapter/signer"
)

const adminSecret = "admin-secret-0123456789abcdef0123456789"

func TestAdminTokens(t *testing.T) {
	a, err := signer.NewAdminTokens(adminSecret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	tok := a.Issue(now.Add(8 * time.Hour))
	if !strings.HasPrefix(tok, "adm1.") {
		t.Fatalf("token = %q", tok)
	}
	if !a.Verify(tok, now) || !a.Verify(tok, now.Add(8*time.Hour-time.Second)) {
		t.Fatal("token sah ditolak")
	}
	if a.Verify(tok, now.Add(8*time.Hour)) {
		t.Fatal("token kedaluwarsa diterima")
	}
	other, _ := signer.NewAdminTokens(adminSecret + "x")
	if other.Verify(tok, now) {
		t.Fatal("token dari secret lain diterima")
	}
	parts := strings.Split(tok, ".")
	exp := parts[1]
	bad := []string{
		"", "adm1.", "adm1." + exp, "adm2." + exp + "." + parts[2],
		"adm1.+" + exp + "." + parts[2], "adm1.0" + exp + "." + parts[2],
		"adm1." + exp + "9." + parts[2], // perpanjang masa berlaku tanpa tanda tangan baru
		"adm1." + exp + "." + parts[2] + "A", "adm1." + exp + ".!!!",
	}
	for _, b := range bad {
		if a.Verify(b, now) {
			t.Fatalf("token rusak diterima: %q", b)
		}
	}
	if _, err := signer.NewAdminTokens("pendek"); err == nil {
		t.Fatal("secret pendek harus ditolak")
	}
}
