package domain

import (
	"regexp"
	"strings"
)

// phoneCanonRe adalah bentuk kanonik nomor seluler Indonesia: 62 8xx…, total 11–15 digit.
// Pola yang sama dipakai di CHECK constraint database.
var phoneCanonRe = regexp.MustCompile(`^628[0-9]{8,12}$`)

func invalidPhone() error {
	return Invalid("INVALID_PHONE", "phone must be an Indonesian mobile number (08…, +628… or 628…)")
}

// NormalizePhone mengubah nomor seluler Indonesia ke bentuk kanonik "628…" (tanpa "+", spasi, atau
// tanda hubung). Hanya karakter yang lazim pada penulisan nomor yang diterima: angka, spasi, "-",
// ".", "(", ")" dan satu "+" di awal. Apa pun selain itu (huruf, "@", JID WhatsApp seperti
// "…@g.us" yang bisa menunjuk grup, dsb.) ditolak, karena nilai ini akan menjadi tujuan pengiriman
// pesan; wa-notif menerima JID mentah, jadi API ini tidak boleh meneruskan input pengguna apa adanya.
//
// Bentuk yang diterima: 08123456789, +628123456789, 628123456789, 8123456789, 0062812…, serta
// "+62 0812…" (kesalahan umum menyertakan 0 setelah kode negara).
func NormalizePhone(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 40 {
		return "", invalidPhone()
	}
	plus := strings.HasPrefix(s, "+")
	if plus {
		s = s[1:]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '.' || r == '(' || r == ')':
			// pemisah yang lazim: diabaikan
		default:
			return "", invalidPhone() // termasuk "+" di tengah, huruf, "@", dan karakter non-ASCII
		}
	}
	d := b.String()
	switch {
	case plus:
		if !strings.HasPrefix(d, "62") {
			return "", invalidPhone() // "+" harus diikuti kode negara Indonesia
		}
	case strings.HasPrefix(d, "0062"): // awalan internasional "00"
		d = d[2:]
	case strings.HasPrefix(d, "00"): // awalan internasional selain 62: bukan nomor Indonesia
		return "", invalidPhone()
	case strings.HasPrefix(d, "0"): // 08xx → 628xx
		d = "62" + d[1:]
	case strings.HasPrefix(d, "8"): // 8xx → 628xx
		d = "62" + d
	}
	if strings.HasPrefix(d, "620") { // kesalahan umum: 0 sesudah kode negara ("+62 0812…")
		d = "62" + d[3:]
	}
	if !phoneCanonRe.MatchString(d) {
		return "", invalidPhone()
	}
	return d, nil
}

// IsCanonicalPhone memeriksa bahwa nilai sudah berbentuk kanonik (dipakai sebagai pagar terakhir
// sebelum nomor dikirim ke wa-notif).
func IsCanonicalPhone(s string) bool { return phoneCanonRe.MatchString(s) }

// MaskPhone menyamarkan nomor kanonik untuk log dan respons: 5 digit pertama dan 4 terakhir
// dipertahankan ("62812****7890"). Nilai yang bukan nomor kanonik disamarkan seluruhnya.
func MaskPhone(p string) string {
	if !IsCanonicalPhone(p) {
		return "****"
	}
	return p[:5] + strings.Repeat("*", len(p)-9) + p[len(p)-4:]
}
