package usecase_test

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/usecase"
)

func newAdminAuth(t *testing.T, clock *fakeClock) *usecase.AdminAuth {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("rahasia-admin"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := signer.NewAdminTokens("admin-secret-0123456789abcdef0123456789")
	return usecase.NewAdminAuth(tokens, clock, usecase.AdminConfig{Username: "admin", PasswordBcrypt: string(hash), TokenTTL: 8 * time.Hour})
}

func TestAdminLogin(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	a := newAdminAuth(t, clock)
	tok, exp, err := a.Login(bg, "admin", "rahasia-admin")
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(clock.t.Add(8*time.Hour)) || !a.VerifyToken(tok) {
		t.Fatalf("exp = %v", exp)
	}
	clock.t = exp
	if a.VerifyToken(tok) {
		t.Fatal("token kedaluwarsa diterima")
	}

	var msgs []string
	for _, c := range []struct{ u, p string }{
		{"admin", "salah"}, {"bukan-admin", "rahasia-admin"}, {"", ""}, {"Admin", "rahasia-admin"},
		{"admin", "rahasia-admin "}, {"admin", string(make([]byte, 100))},
	} {
		_, _, err := a.Login(bg, c.u, c.p)
		if errCode(err) != "INVALID_CREDENTIALS" {
			t.Fatalf("%q/%q: %v", c.u, c.p, err)
		}
		msgs = append(msgs, err.Error())
	}
	for _, m := range msgs[1:] {
		if m != msgs[0] {
			t.Fatalf("pesan berbeda: %q vs %q", m, msgs[0])
		}
	}
	for _, bad := range []string{"", "Bearer x", "adm1.123.abc"} {
		if a.VerifyToken(bad) {
			t.Fatalf("%q diterima", bad)
		}
	}
}
