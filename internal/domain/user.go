package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxNameLen  = 100
	MaxEmailLen = 254
)

type UserStatus string

const (
	UserPending UserStatus = "pending" // nomor HP belum diverifikasi (OTP pertama)
	UserActive  UserStatus = "active"
)

// User adalah pengguna terdaftar (auth.mode = otp). Identitas diverifikasi lewat nomor HP;
// email hanya data kontak (tidak diverifikasi dan tidak harus unik).
type User struct {
	ID              uuid.UUID
	Name            string
	Email           string
	Phone           string // bentuk kanonik 628…
	Status          UserStatus
	PhoneVerifiedAt *time.Time
	CreatedAt       time.Time
	LastLoginAt     *time.Time
}

// NormalizeName memangkas dan memvalidasi nama: 1–100 karakter, UTF-8 valid, tanpa karakter
// kontrol (termasuk newline) dan tanpa karakter kontrol arah teks.
func NormalizeName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", Invalid("INVALID_NAME", "name must be valid UTF-8")
	}
	s := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(s); n < 1 || n > MaxNameLen {
		return "", Invalid("INVALID_NAME", "name must be between 1 and %d characters", MaxNameLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) || isBidiControl(r) {
			return "", Invalid("INVALID_NAME", "name contains a disallowed character")
		}
	}
	return s, nil
}

// NormalizeEmail memvalidasi bentuk alamat email dan mengembalikannya dalam huruf kecil. Email
// tidak diverifikasi, jadi pemeriksaan sengaja sederhana: satu alamat polos (tanpa nama tampilan
// atau komentar), ada domain bertitik, tanpa spasi atau karakter kontrol.
func NormalizeEmail(raw string) (string, error) {
	bad := func() (string, error) { return "", Invalid("INVALID_EMAIL", "email address is not valid") }
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > MaxEmailLen || !utf8.ValidString(s) {
		return bad()
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || isBidiControl(r) || r == '<' || r == '>' || r == '(' || r == ')' || r == ',' || r == ';' || r == '"' {
			return bad()
		}
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return bad()
	}
	at := strings.LastIndex(s, "@")
	if at < 1 || !strings.Contains(s[at+1:], ".") || strings.HasPrefix(s[at+1:], ".") || strings.HasSuffix(s[at+1:], ".") {
		return bad()
	}
	return strings.ToLower(s), nil
}
