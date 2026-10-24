package domain

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MinBBoxSize adalah lebar/tinggi minimum kotak (ternormalisasi) agar kotak tak sengaja ditolak.
	MinBBoxSize = 0.005

	// bboxEpsilon menoleransi pembulatan floating point (mis. piksel→0..1 di browser) pada
	// batas x+w ≤ 1 dan y+h ≤ 1. Konstanta yang sama dipakai di CHECK constraint database.
	bboxEpsilon = 1e-6
)

// BBoxEpsilon diekspos agar migrasi dan test merujuk nilai yang sama dengan domain.
const BBoxEpsilon = bboxEpsilon

// Annotation adalah satu kotak berteks pada sebuah foto. Koordinat dinormalisasi 0–1 relatif
// terhadap foto hasil proses, origin kiri atas (spec §6.3).
type Annotation struct {
	Text string
	X, Y float64 // pojok kiri atas
	W, H float64 // lebar dan tinggi
}

// ValidateBBox memeriksa geometri kotak.
func (a Annotation) ValidateBBox() error {
	for _, v := range [...]float64{a.X, a.Y, a.W, a.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Invalid("INVALID_BBOX", "bounding box coordinates must be finite numbers")
		}
	}
	switch {
	case a.X < 0 || a.X >= 1 || a.Y < 0 || a.Y >= 1:
		return Invalid("INVALID_BBOX", "x and y must satisfy 0 <= value < 1")
	case a.W <= 0 || a.W > 1 || a.H <= 0 || a.H > 1:
		return Invalid("INVALID_BBOX", "w and h must satisfy 0 < value <= 1")
	case a.W < MinBBoxSize || a.H < MinBBoxSize:
		return Invalid("INVALID_BBOX", "w and h must be at least %v", MinBBoxSize)
	case a.X+a.W > 1+bboxEpsilon || a.Y+a.H > 1+bboxEpsilon:
		return Invalid("INVALID_BBOX", "the box must stay inside the photo (x+w <= 1 and y+h <= 1)")
	}
	return nil
}

// NormalizeAnnotationText merapikan dan memvalidasi teks anotasi: CRLF→LF, trim, 1..maxLen
// rune, UTF-8 valid, tanpa karakter kontrol selain newline dan tanpa karakter kontrol arah
// teks (bidi override/isolate) yang bisa dipakai menipu tampilan di layar admin.
func NormalizeAnnotationText(s string, maxLen int) (string, error) {
	if !utf8.ValidString(s) {
		return "", Invalid("INVALID_ANNOTATION_TEXT", "text must be valid UTF-8")
	}
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	n := utf8.RuneCountInString(s)
	if n < 1 || n > maxLen {
		return "", Invalid("INVALID_ANNOTATION_TEXT", "text must be between 1 and %d characters after trimming", maxLen)
	}
	for _, r := range s {
		if r == '\n' {
			continue
		}
		if unicode.IsControl(r) || isBidiControl(r) {
			return "", Invalid("INVALID_ANNOTATION_TEXT", "text contains a disallowed control character")
		}
	}
	return s, nil
}

func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// Validate memeriksa seluruh anotasi dan mengembalikan salinan dengan teks yang sudah dinormalisasi.
func (a Annotation) Validate(maxTextLen int) (Annotation, error) {
	if err := a.ValidateBBox(); err != nil {
		return Annotation{}, err
	}
	t, err := NormalizeAnnotationText(a.Text, maxTextLen)
	if err != nil {
		return Annotation{}, err
	}
	a.Text = t
	return a, nil
}
