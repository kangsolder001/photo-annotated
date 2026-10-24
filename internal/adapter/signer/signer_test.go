package signer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/signer"
)

const secret = "0123456789abcdef0123456789abcdef0123456789abcdef"

var (
	id  = uuid.MustParse("0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11")
	exp = time.Date(2026, 10, 24, 12, 10, 0, 0, time.UTC)
)

func newSigner(t *testing.T, sec string) *signer.Signer {
	t.Helper()
	s, err := signer.New(sec)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewRejectsShortSecret(t *testing.T) {
	for _, s := range []string{"", "pendek", strings.Repeat("x", 31)} {
		if _, err := signer.New(s); err == nil {
			t.Errorf("secret %d karakter harus ditolak", len(s))
		}
	}
	if _, err := signer.New(strings.Repeat("x", 32)); err != nil {
		t.Errorf("32 karakter harus diterima: %v", err)
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	s := newSigner(t, secret)
	sig := s.SignUpload(id, exp, "image/webp", 412345)
	if len(sig) != 43 { // SHA-256 → 32 byte → 43 karakter base64url tanpa padding
		t.Fatalf("panjang sig = %d", len(sig))
	}
	if strings.ContainsAny(sig, "+/=") {
		t.Fatalf("sig harus aman di URL: %q", sig)
	}
	if !s.VerifyUpload(id, exp.Unix(), "image/webp", 412345, sig) {
		t.Fatal("tanda tangan sah harus lolos")
	}
	if s.SignUpload(id, exp, "image/webp", 412345) != sig {
		t.Fatal("tanda tangan harus deterministik")
	}
}

func TestEveryFieldIsBound(t *testing.T) {
	s := newSigner(t, secret)
	sig := s.SignUpload(id, exp, "image/webp", 412345)
	other := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	for name, ok := range map[string]bool{
		"photo id lain":     s.VerifyUpload(other, exp.Unix(), "image/webp", 412345, sig),
		"exp lain":          s.VerifyUpload(id, exp.Unix()+1, "image/webp", 412345, sig),
		"content-type lain": s.VerifyUpload(id, exp.Unix(), "image/jpeg", 412345, sig),
		"ukuran lain":       s.VerifyUpload(id, exp.Unix(), "image/webp", 412346, sig),
		"secret lain":       newSigner(t, strings.Repeat("z", 48)).VerifyUpload(id, exp.Unix(), "image/webp", 412345, sig),
	} {
		if ok {
			t.Errorf("%s: tanda tangan tidak boleh lolos", name)
		}
	}
}

func TestMalformedSignaturesRejected(t *testing.T) {
	s := newSigner(t, secret)
	good := s.SignUpload(id, exp, "image/webp", 1)
	for name, sig := range map[string]string{
		"kosong":          "",
		"bukan base64":    "!!!!",
		"terlalu pendek":  good[:20],
		"terlalu panjang": good + "AAAA",
		"dengan padding":  good + "=",
		"huruf diubah":    "A" + good[1:],
		"standar base64":  strings.NewReplacer("-", "+", "_", "/").Replace(good) + "==",
		"spasi":           " " + good,
	} {
		if sig == good {
			continue // penggantian tak mengubah nilai
		}
		if s.VerifyUpload(id, exp.Unix(), "image/webp", 1, sig) {
			t.Errorf("%s: seharusnya ditolak (%q)", name, sig)
		}
	}
}

// ---------------------------------------------------------------- token edit

func TestEditTokens(t *testing.T) {
	et, err := signer.NewEditTokens(secret)
	if err != nil {
		t.Fatal(err)
	}
	tok := et.Token(id)
	if len(tok) != 43 || strings.ContainsAny(tok, "+/=") {
		t.Fatalf("token = %q", tok)
	}
	if et.Token(id) != tok {
		t.Fatal("token harus deterministik (replay idempoten mengembalikan token yang sama)")
	}
	if !et.Verify(id, tok) {
		t.Fatal("token sah harus lolos")
	}
	other := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	if et.Verify(other, tok) {
		t.Fatal("token submission lain tidak boleh lolos")
	}
	if et.Token(other) == tok {
		t.Fatal("token harus berbeda per submission")
	}
	for name, bad := range map[string]string{"kosong": "", "sampah": "!!!", "pendek": tok[:10], "panjang": tok + "AA", "padding": tok + "="} {
		if et.Verify(id, bad) {
			t.Errorf("%s: seharusnya ditolak", name)
		}
	}
	changed := "A" + tok[1:]
	if tok[0] == 'A' {
		changed = "B" + tok[1:]
	}
	if et.Verify(id, changed) {
		t.Fatal("token diubah harus ditolak")
	}
	et2, _ := signer.NewEditTokens(strings.Repeat("z", 48))
	if et2.Verify(id, tok) {
		t.Fatal("secret berbeda (rotasi) harus membatalkan token lama")
	}
	if _, err := signer.NewEditTokens("pendek"); err == nil {
		t.Fatal("secret pendek harus ditolak")
	}
}

// Token edit dan tanda tangan URL memakai pesan berawalan berbeda: satu tidak berlaku untuk lainnya,
// bahkan bila kedua secret kebetulan sama.
func TestEditTokenAndUploadSignatureAreSeparateDomains(t *testing.T) {
	et, _ := signer.NewEditTokens(secret)
	s := newSigner(t, secret)
	tok := et.Token(id)
	if s.VerifyUpload(id, exp.Unix(), "image/webp", 1, tok) {
		t.Fatal("token edit tidak boleh lolos sebagai tanda tangan upload")
	}
	sig := s.SignUpload(id, exp, "image/webp", 1)
	if et.Verify(id, sig) {
		t.Fatal("tanda tangan upload tidak boleh lolos sebagai token edit")
	}
}

// ---------------------------------------------------------------- tanda tangan lihat foto

func TestSignVerifyView(t *testing.T) {
	s := newSigner(t, secret)
	sig := s.SignView(id, exp)
	if len(sig) != 43 || strings.ContainsAny(sig, "+/=") {
		t.Fatalf("sig = %q", sig)
	}
	if !s.VerifyView(id, exp.Unix(), sig) {
		t.Fatal("tanda tangan sah harus lolos")
	}
	other := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	for name, ok := range map[string]bool{
		"foto lain":   s.VerifyView(other, exp.Unix(), sig),
		"exp lain":    s.VerifyView(id, exp.Unix()+1, sig),
		"secret lain": newSigner(t, strings.Repeat("z", 48)).VerifyView(id, exp.Unix(), sig),
		"kosong":      s.VerifyView(id, exp.Unix(), ""),
		"sampah":      s.VerifyView(id, exp.Unix(), "!!!"),
		"pendek":      s.VerifyView(id, exp.Unix(), sig[:20]),
		"padding":     s.VerifyView(id, exp.Unix(), sig+"="),
	} {
		if ok {
			t.Errorf("%s: tidak boleh lolos", name)
		}
	}
}

// Satu tujuan tidak boleh dipakai untuk tujuan lain: URL lihat foto tidak bisa dipakai untuk
// upload, dan sebaliknya.
func TestViewAndUploadSignaturesAreSeparateDomains(t *testing.T) {
	s := newSigner(t, secret)
	view := s.SignView(id, exp)
	if s.VerifyUpload(id, exp.Unix(), "image/webp", 1, view) {
		t.Fatal("tanda tangan lihat tidak boleh lolos sebagai upload")
	}
	up := s.SignUpload(id, exp, "image/webp", 1)
	if s.VerifyView(id, exp.Unix(), up) {
		t.Fatal("tanda tangan upload tidak boleh lolos sebagai lihat")
	}
}
