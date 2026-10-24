package domain_test

import (
	"strings"
	"testing"
	"time"

	"photo-annotated/internal/domain"
)

func TestNormalizePhoneAcceptedForms(t *testing.T) {
	const want = "6281234567890"
	for _, in := range []string{
		"081234567890", "0812-3456-7890", "0812 3456 7890", "(0812) 3456-7890", "0812.3456.7890",
		"+6281234567890", "+62 812 3456 7890", "+62-812-3456-7890", "6281234567890", "62 812-3456-7890",
		"81234567890", "812 3456 7890",
		"006281234567890", "+62 0812 3456 7890", "620812 3456 7890",
		"  081234567890  ",
	} {
		got, err := domain.NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	// panjang yang berbeda tetap sah selama 11–15 digit
	for in, want := range map[string]string{
		"08123456789":     "628123456789",     // 12 digit
		"0812345678":      "62812345678",      // 11 digit (terpendek)
		"081234567890123": "6281234567890123", // 16 digit → tidak sah (di bawah)
	} {
		got, err := domain.NormalizePhone(in)
		if in == "081234567890123" {
			if err == nil {
				t.Errorf("%q: 16 digit harus ditolak, dapat %q", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if got, err := domain.NormalizePhone("0812345678901"); err != nil || got != "62812345678901" {
		t.Errorf("14 digit: %q %v", got, err)
	}
	if got, err := domain.NormalizePhone("08123456789012"); err != nil || got != "628123456789012" {
		t.Errorf("15 digit (terpanjang): %q %v", got, err)
	}
}

func TestNormalizePhoneRejections(t *testing.T) {
	for name, in := range map[string]string{
		"kosong":                       "",
		"hanya spasi":                  "   ",
		"JID grup":                     "6281234567890-1600000000@g.us",
		"JID pengguna":                 "6281234567890@s.whatsapp.net",
		"mengandung @":                 "0812@3456",
		"huruf":                        "0812abc7890",
		"huruf di ujung":               "081234567890 ext 5",
		"plus di tengah":               "0812+34567890",
		"dua plus":                     "++6281234567890",
		"plus tanpa kode negara":       "+81234567890",
		"plus dengan kode negara lain": "+1 415 555 2671",
		"nomor negara lain":            "00 1 415 555 2671",
		"telepon rumah Jakarta":        "02112345678",
		"62 tetapi bukan seluler":      "622112345678",
		"terlalu pendek":               "08123",
		"terlalu panjang":              "0812345678901234",
		"kosong setelah plus":          "+",
		"karakter kontrol":             "0812\x003456",
		"newline":                      "0812\n34567890",
		"tab":                          "0812\t34567890",
		"angka non-ASCII":              "٠٨١٢٣٤٥٦٧٨٩٠",
		"bidi override":                "0812‮34567890",
		"terlalu panjang sebagai teks": "0812" + strings.Repeat(" ", 60) + "34567890",
		"sql injection":                "0812'; DROP TABLE users;--",
		"awalan 0 ganda":               "00812345678900",
	} {
		got, err := domain.NormalizePhone(in)
		if err == nil {
			t.Errorf("%s: %q seharusnya ditolak, dapat %q", name, in, got)
			continue
		}
		de, ok := domain.AsError(err)
		if !ok || de.Code != "INVALID_PHONE" || de.Kind != domain.KindInvalid {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestNormalizePhoneResultIsAlwaysCanonical(t *testing.T) {
	for _, in := range []string{"0812-3456-7890", "+62 812 3456 7890", "81234567890", "620812 3456 7890"} {
		got, err := domain.NormalizePhone(in)
		if err != nil || !domain.IsCanonicalPhone(got) {
			t.Fatalf("%q → %q (%v) harus kanonik", in, got, err)
		}
		// idempoten
		again, err := domain.NormalizePhone(got)
		if err != nil || again != got {
			t.Fatalf("normalisasi harus idempoten: %q → %q", got, again)
		}
	}
	for _, bad := range []string{"", "08123456789", "+6281234567890", "6281234567890 ", "6281234567890\n", "62812345678901234", "6212345678901", "abc"} {
		if domain.IsCanonicalPhone(bad) {
			t.Errorf("IsCanonicalPhone(%q) seharusnya false", bad)
		}
	}
}

func TestMaskPhone(t *testing.T) {
	for in, want := range map[string]string{
		"6281234567890":   "62812****7890", // contoh di spec
		"62812345678":     "62812**5678",
		"628123456789012": "62812******9012",
		"08123456789":     "****", // bukan kanonik → samarkan seluruhnya
		"":                "****",
		"abc":             "****",
	} {
		if got := domain.MaskPhone(in); got != want {
			t.Errorf("MaskPhone(%q) = %q, want %q", in, got, want)
		}
	}
	if got := domain.MaskPhone("6281234567890"); strings.Contains(got, "3456") {
		t.Fatal("bagian tengah nomor tidak boleh terlihat")
	}
}

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]string{
		"Budi Santoso":           "Budi Santoso",
		"  Budi  ":               "Budi",
		"Ismi Nur Aziza":         "Ismi Nur Aziza",
		"李小龍":                    "李小龍",
		"Nyoman Ayu Kusuma Dewi": "Nyoman Ayu Kusuma Dewi",
		"A":                      "A",
		strings.Repeat("あ", 100): strings.Repeat("あ", 100),
		"Mary-Jane O'Neil":       "Mary-Jane O'Neil",
	} {
		got, err := domain.NormalizeName(in)
		if err != nil || got != want {
			t.Errorf("NormalizeName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for name, in := range map[string]string{
		"kosong":        "",
		"spasi saja":    "    ",
		"101 karakter":  strings.Repeat("x", 101),
		"101 rune":      strings.Repeat("あ", 101),
		"newline":       "Budi\nSantoso",
		"tab":           "Budi\tSantoso",
		"null":          "Bu\x00di",
		"escape":        "Bu\x1bdi",
		"UTF-8 rusak":   "Bu\xffdi",
		"bidi override": "Budi‮Santoso",
		"isolate bidi":  "Bu⁦di",
	} {
		_, err := domain.NormalizeName(in)
		de, ok := domain.AsError(err)
		if !ok || de.Code != "INVALID_NAME" || de.Kind != domain.KindInvalid {
			t.Errorf("%s: %q err = %v", name, in, err)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	for in, want := range map[string]string{
		"budi@example.com":           "budi@example.com",
		"Budi.Santoso@Example.COM":   "budi.santoso@example.com",
		"  budi@example.com  ":       "budi@example.com",
		"budi@example.com\r\n":       "budi@example.com", // spasi/CRLF di ujung dipangkas (yang di tengah ditolak)
		"budi+foto@example.co.id":    "budi+foto@example.co.id",
		"b_u-d.i@sub.domain.example": "b_u-d.i@sub.domain.example",
		"a@b.co":                     "a@b.co",
	} {
		got, err := domain.NormalizeEmail(in)
		if err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	long := strings.Repeat("a", 250) + "@b.co"
	for name, in := range map[string]string{
		"kosong":                   "",
		"tanpa @":                  "budi.example.com",
		"tanpa domain":             "budi@",
		"tanpa local part":         "@example.com",
		"domain tanpa titik":       "budi@localhost",
		"domain berawalan titik":   "budi@.example.com",
		"domain berakhiran titik":  "budi@example.com.",
		"dua @":                    "a@b@example.com",
		"nama tampilan":            "Budi <budi@example.com>",
		"dengan spasi":             "bu di@example.com",
		"koma / dua alamat":        "a@example.com,b@example.com",
		"titik koma":               "a@example.com;b@example.com",
		"kurung / komentar":        "budi(komentar)@example.com",
		"kutip":                    `"budi"@example.com`,
		"newline (injeksi header)": "budi@example.com\nBcc: x@y.com",
		"null":                     "budi@exa\x00mple.com",
		"UTF-8 rusak":              "budi@exa\xffmple.com",
		"255 karakter":             long,
		"bidi override":            "budi@example‮.com",
	} {
		if _, err := domain.NormalizeEmail(in); err == nil {
			t.Errorf("%s: %q seharusnya ditolak", name, in)
		} else if de, ok := domain.AsError(err); !ok || de.Code != "INVALID_EMAIL" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestOTPStatusActive(t *testing.T) {
	for st, want := range map[domain.OTPStatus]bool{
		domain.OTPQueued: true, domain.OTPSending: true, domain.OTPSent: true,
		domain.OTPFailed: false, domain.OTPConsumed: false, domain.OTPExpired: false, "": false,
	} {
		if st.Active() != want {
			t.Errorf("%q.Active() = %v, want %v", st, st.Active(), want)
		}
	}
}

func TestSessionActive(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	revoked := now.Add(-time.Minute)
	for name, tc := range map[string]struct {
		s    domain.Session
		want bool
	}{
		"berlaku":                  {domain.Session{ExpiresAt: now.Add(time.Hour)}, true},
		"sedetik sebelum berakhir": {domain.Session{ExpiresAt: now.Add(time.Nanosecond)}, true},
		"tepat pada batas":         {domain.Session{ExpiresAt: now}, false},
		"kedaluwarsa":              {domain.Session{ExpiresAt: now.Add(-time.Second)}, false},
		"dicabut":                  {domain.Session{ExpiresAt: now.Add(time.Hour), RevokedAt: &revoked}, false},
	} {
		if got := tc.s.Active(now); got != tc.want {
			t.Errorf("%s: Active = %v, want %v", name, got, tc.want)
		}
	}
}
