package signer_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"photo-annotated/internal/adapter/signer"
)

const (
	adminSecret = "admin-secret-0123456789abcdef0123456789"
	adminPhone  = "6281234567890"
)

func TestAdminTokens(t *testing.T) {
	a, err := signer.NewAdminTokens(adminSecret, adminPhone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	tok := a.Issue(now.Add(8 * time.Hour))
	if !strings.HasPrefix(tok, "adm2.") {
		t.Fatalf("token = %q", tok)
	}
	if exp, ok := a.Verify(tok, now); !ok || !exp.Equal(now.Add(8*time.Hour)) {
		t.Fatalf("token sah ditolak: %v %v", exp, ok)
	}
	if _, ok := a.Verify(tok, now.Add(8*time.Hour-time.Second)); !ok {
		t.Fatal("token sah ditolak sesaat sebelum kedaluwarsa")
	}
	if _, ok := a.Verify(tok, now.Add(8*time.Hour)); ok {
		t.Fatal("token kedaluwarsa diterima")
	}
	other, _ := signer.NewAdminTokens(adminSecret+"x", adminPhone)
	if _, ok := other.Verify(tok, now); ok {
		t.Fatal("token dari secret lain diterima")
	}
	// mengganti nomor superadmin membatalkan semua token lama
	otherPhone, _ := signer.NewAdminTokens(adminSecret, "6281234567891")
	if _, ok := otherPhone.Verify(tok, now); ok {
		t.Fatal("token untuk nomor superadmin lain diterima")
	}
	parts := strings.Split(tok, ".")
	exp := parts[1]
	// token adm1 lama (HMAC tanpa nomor) dengan secret yang sama tetap ditolak
	m := hmac.New(sha256.New, []byte(adminSecret))
	m.Write([]byte("admin\n" + exp))
	adm1 := "adm1." + exp + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	bad := []string{
		"", "adm2.", "adm2." + exp, "adm1." + exp + "." + parts[2], adm1,
		"adm2.+" + exp + "." + parts[2], "adm2.0" + exp + "." + parts[2],
		"adm2." + exp + "9." + parts[2], // perpanjang masa berlaku tanpa tanda tangan baru
		"adm2." + exp + "." + parts[2] + "A", "adm2." + exp + ".!!!",
	}
	for _, b := range bad {
		if _, ok := a.Verify(b, now); ok {
			t.Fatalf("token rusak diterima: %q", b)
		}
	}
	if _, err := signer.NewAdminTokens("pendek", adminPhone); err == nil {
		t.Fatal("secret pendek harus ditolak")
	}
	if _, err := signer.NewAdminTokens(adminSecret, "0812-3456-7890"); err == nil {
		t.Fatal("nomor non-kanonik harus ditolak")
	}
}
