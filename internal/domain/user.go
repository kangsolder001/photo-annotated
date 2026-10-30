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
	// PasswordHash adalah hash bcrypt; kosong bila pengguna belum pernah membuat password.
	PasswordHash string
	// BlockedAt diisi admin saat memblokir annotator (spec-labels-admin §5.8); nil = tidak diblokir.
	BlockedAt     *time.Time
	BlockedReason string
	// NIM (huruf besar) dan kelompok mahasiswa (spec-pendataan §5.4); kosong/nil untuk akun lama.
	NIM       string
	GroupID   *int
	GroupName string // nama kelompok terkini (join), kosong bila GroupID nil
}

// Blocked: akun diblokir admin. Sesi ditolak dan login dengan password benar → ACCOUNT_BLOCKED.
func (u *User) Blocked() bool { return u.BlockedAt != nil }

const (
	MinPasswordLen = 8
	// MaxPasswordBytes: bcrypt hanya memakai 72 byte pertama; password yang lebih panjang ditolak
	// agar dua password berbeda tidak dianggap sama.
	MaxPasswordBytes = 72
)

// ValidatePassword: 8 karakter s.d. 72 byte, UTF-8 valid, tidak hanya spasi, tanpa karakter kontrol.
// Sengaja tanpa aturan komposisi (huruf besar/simbol) sesuai NIST SP 800-63B.
func ValidatePassword(p string) error {
	bad := func(format string, a ...any) error { return Invalid("INVALID_PASSWORD", format, a...) }
	if !utf8.ValidString(p) {
		return bad("password must be valid UTF-8")
	}
	if utf8.RuneCountInString(p) < MinPasswordLen {
		return bad("password must be at least %d characters", MinPasswordLen)
	}
	if len(p) > MaxPasswordBytes {
		return bad("password must be at most %d bytes", MaxPasswordBytes)
	}
	if strings.TrimSpace(p) == "" {
		return bad("password must not be only spaces")
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return bad("password contains a control character")
		}
	}
	return nil
}

// NormalizeNIM: trim, huruf besar, 5–20 huruf/angka ASCII (contoh 251FS01017). Salah → INVALID_NIM.
func NormalizeNIM(raw string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if n := len(s); n < 5 || n > 20 {
		return "", Invalid("INVALID_NIM", "NIM must be 5-20 letters or digits")
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return "", Invalid("INVALID_NIM", "NIM must be 5-20 letters or digits")
		}
	}
	return s, nil
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
