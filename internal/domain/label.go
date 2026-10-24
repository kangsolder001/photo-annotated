package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxLabelNameLen        = 50
	MaxLabelDescriptionLen = 200
)

// Label adalah satu kelas anotasi yang dikelola admin (spec-labels-admin §4). Anotasi menyimpan ID,
// sehingga mengganti nama/warna berlaku ke semua anotasi yang memakainya.
type Label struct {
	ID          int // dibuat server, tidak pernah dipakai ulang
	Name        string
	Description string // kosong = tanpa deskripsi
	Color       string // #rrggbb huruf kecil
	SortOrder   int
	ArchivedAt  *time.Time // nil = aktif
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Archived: label tidak muncul di pilihan pengguna, tetapi tetap tampil di anotasi lama dan export.
func (l *Label) Archived() bool { return l.ArchivedAt != nil }

// LabelPalette adalah warna bawaan yang dipilih bergiliran bila admin tidak memberi warna.
var LabelPalette = []string{
	"#e4572e", "#2bb3a3", "#f3a712", "#4c6ef5", "#a23b72", "#3bb273",
	"#7768ae", "#d1495b", "#1c7c9c", "#c97c10", "#5c946e", "#e07be0",
}

var labelColorRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// NormalizeLabelName: trim, 1–50 karakter, tanpa karakter kontrol (termasuk newline) dan tanpa
// karakter kontrol arah teks.
func NormalizeLabelName(raw string) (string, error) {
	s, ok := cleanLine(raw, 1, MaxLabelNameLen)
	if !ok {
		return "", Invalid("INVALID_LABEL_NAME", "label name must be 1-%d characters without control characters", MaxLabelNameLen)
	}
	return s, nil
}

// NormalizeLabelDescription: opsional (kosong setelah trim = tanpa deskripsi), maksimal 200 karakter,
// tanpa karakter kontrol.
func NormalizeLabelDescription(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	s, ok := cleanLine(raw, 1, MaxLabelDescriptionLen)
	if !ok {
		return "", Invalid("INVALID_LABEL_DESCRIPTION", "label description must be at most %d characters without control characters", MaxLabelDescriptionLen)
	}
	return s, nil
}

// NormalizeLabelColor menerima #rrggbb (huruf besar dijadikan kecil).
func NormalizeLabelColor(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if !labelColorRe.MatchString(s) {
		return "", Invalid("INVALID_LABEL_COLOR", "label color must be #rrggbb")
	}
	return s, nil
}

// NormalizeReason memvalidasi alasan teks bebas dari admin (alasan review/blokir): trim, 1–500
// karakter, tanpa karakter kontrol/bidi (aturan sama dengan nama label). code = kode error.
func NormalizeReason(raw, code string) (string, error) {
	s, ok := cleanLine(raw, 1, 500)
	if !ok {
		return "", Invalid(code, "reason must be 1-500 characters without control characters")
	}
	return s, nil
}

// cleanLine: UTF-8 valid, trim, panjang min–max rune, tanpa karakter kontrol atau bidi.
func cleanLine(raw string, min, max int) (string, bool) {
	if !utf8.ValidString(raw) {
		return "", false
	}
	s := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(s); n < min || n > max {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) || isBidiControl(r) {
			return "", false
		}
	}
	return s, true
}
