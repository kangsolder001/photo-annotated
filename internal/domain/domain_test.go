package domain_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok {
		t.Fatalf("bukan *domain.Error: %v", err)
	}
	return de.Code
}

// ---------------------------------------------------------------- errors

func TestErrorKindsAndIs(t *testing.T) {
	wrapped := errors.Join(errors.New("konteks"), domain.NotFound("NOT_FOUND", "foto %d tidak ada", 7))
	if !errors.Is(wrapped, domain.ErrNotFound) {
		t.Fatal("errors.Is harus cocok berdasarkan Code")
	}
	if errors.Is(domain.Invalid("X", "y"), domain.ErrNotFound) {
		t.Fatal("Code berbeda tidak boleh cocok")
	}
	de, ok := domain.AsError(wrapped)
	if !ok || de.Kind != domain.KindNotFound || de.Message != "foto 7 tidak ada" {
		t.Fatalf("AsError: %+v %v", de, ok)
	}
	if _, ok := domain.AsError(errors.New("biasa")); ok {
		t.Fatal("error biasa bukan domain error")
	}
}

// ---------------------------------------------------------------- upload spec

func TestValidateUploadSpec(t *testing.T) {
	const max = 2 << 20
	tests := []struct {
		name     string
		ct       string
		size     int64
		w, h     int
		wantCode string
	}{
		{"webp ok", "image/webp", 412345, 1920, 1440, ""},
		{"jpeg ok", "image/jpeg", 1, 1, 1, ""},
		{"ukuran tepat batas", "image/webp", max, 2048, 100, ""},
		{"sisi tepat 2048", "image/jpeg", 100, 2048, 2048, ""},
		{"png ditolak", "image/png", 100, 10, 10, "INVALID_CONTENT_TYPE"},
		{"content-type kosong", "", 100, 10, 10, "INVALID_CONTENT_TYPE"},
		{"huruf besar ditolak", "IMAGE/WEBP", 100, 10, 10, "INVALID_CONTENT_TYPE"},
		{"parameter charset ditolak", "image/webp; charset=x", 100, 10, 10, "INVALID_CONTENT_TYPE"},
		{"ukuran 0", "image/webp", 0, 10, 10, "INVALID_SIZE"},
		{"ukuran negatif", "image/webp", -5, 10, 10, "INVALID_SIZE"},
		{"ukuran melebihi batas", "image/webp", max + 1, 10, 10, "INVALID_SIZE"},
		{"lebar 0", "image/webp", 100, 0, 10, "INVALID_DIMENSIONS"},
		{"tinggi negatif", "image/webp", 100, 10, -1, "INVALID_DIMENSIONS"},
		{"sisi 2049", "image/webp", 100, 2049, 10, "INVALID_DIMENSIONS"},
		{"tinggi 2049", "image/webp", 100, 10, 2049, "INVALID_DIMENSIONS"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateUploadSpec(tc.ct, tc.size, tc.w, tc.h, max)
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("seharusnya valid: %v", err)
				}
				return
			}
			if err == nil || codeOf(t, err) != tc.wantCode {
				t.Fatalf("want %s, got %v", tc.wantCode, err)
			}
			if de, _ := domain.AsError(err); de.Kind != domain.KindInvalid {
				t.Fatalf("Kind = %v, want KindInvalid", de.Kind)
			}
		})
	}
}

// ---------------------------------------------------------------- storage key

func TestNewStorageKey(t *testing.T) {
	id := uuid.MustParse("0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11")
	// 23:30 WIB (UTC+7) tanggal 24 = 16:30 UTC tanggal 24; 02:00 WIB tanggal 25 = 19:00 UTC tanggal 24
	wib := time.FixedZone("WIB", 7*3600)
	for _, tc := range []struct {
		now  time.Time
		ct   string
		want string
	}{
		{time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), "image/webp", "2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp"},
		{time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), "image/jpeg", "2026/01/05/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.jpg"},
		{time.Date(2026, 10, 25, 2, 0, 0, 0, wib), "image/webp", "2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp"}, // dikonversi ke UTC
	} {
		got, err := domain.NewStorageKey(id, tc.now, tc.ct)
		if err != nil || got != tc.want {
			t.Errorf("NewStorageKey = %q, %v; want %q", got, err, tc.want)
		}
		if !domain.IsValidStorageKey(got) {
			t.Errorf("kunci buatan sendiri harus lolos IsValidStorageKey: %q", got)
		}
	}
	if _, err := domain.NewStorageKey(id, time.Now(), "image/png"); err == nil {
		t.Fatal("content-type tak didukung harus ditolak")
	}
}

func TestIsValidStorageKey(t *testing.T) {
	const ok = "2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp"
	if !domain.IsValidStorageKey(ok) {
		t.Fatal("kunci sah ditolak")
	}
	for _, bad := range []string{
		"", "../2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp",
		"/etc/passwd", "2026/10/24/../../../etc/passwd",
		"2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.png",
		"2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp/",
		"2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp\n",
		"2026/10/24/0B6C1C2E-6A5E-4C55-9C0B-2F6C2D8E9A11.webp", // huruf besar: bukan bentuk kanonik
		"26/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp",
		"tmp/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.part",
		"2026/10/24/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11.webp\x00.jpg",
		"2026/10/24/photo.webp",
	} {
		if domain.IsValidStorageKey(bad) {
			t.Errorf("kunci tak sah lolos: %q", bad)
		}
	}
}

// ---------------------------------------------------------------- photo lifecycle

func TestReadyToAttach(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		p        domain.Photo
		wantCode string
		wantKind domain.Kind
	}{
		{"pending + ter-upload", domain.Photo{Status: domain.PhotoPending, UploadedAt: &now}, "", 0},
		{"pending belum upload", domain.Photo{Status: domain.PhotoPending}, "PHOTO_NOT_UPLOADED", domain.KindInvalid},
		{"sudah attached", domain.Photo{Status: domain.PhotoAttached, UploadedAt: &now}, "PHOTO_NOT_AVAILABLE", domain.KindConflict},
		{"rejected", domain.Photo{Status: domain.PhotoRejected, UploadedAt: &now}, "PHOTO_NOT_AVAILABLE", domain.KindConflict},
		{"deleted", domain.Photo{Status: domain.PhotoDeleted, UploadedAt: &now}, "PHOTO_NOT_AVAILABLE", domain.KindConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.ReadyToAttach()
			if tc.wantCode == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			de, ok := domain.AsError(err)
			if !ok || de.Code != tc.wantCode || de.Kind != tc.wantKind {
				t.Fatalf("got %v, want %s/%v", err, tc.wantCode, tc.wantKind)
			}
		})
	}
}

func TestPhotoStatusValid(t *testing.T) {
	for _, s := range []domain.PhotoStatus{domain.PhotoPending, domain.PhotoAttached, domain.PhotoRejected, domain.PhotoDeleted} {
		if !s.Valid() {
			t.Errorf("%q harus valid", s)
		}
	}
	for _, s := range []domain.PhotoStatus{"", "PENDING", "unknown"} {
		if s.Valid() {
			t.Errorf("%q tidak boleh valid", s)
		}
	}
}

// ---------------------------------------------------------------- bbox

func TestValidateBBox(t *testing.T) {
	const eps = domain.BBoxEpsilon
	tests := []struct {
		name string
		a    domain.Annotation
		ok   bool
	}{
		{"kotak biasa", domain.Annotation{X: 0.12, Y: 0.30, W: 0.25, H: 0.18}, true},
		{"seluruh foto", domain.Annotation{X: 0, Y: 0, W: 1, H: 1}, true},
		{"ukuran minimum tepat 0.005", domain.Annotation{X: 0.1, Y: 0.1, W: 0.005, H: 0.005}, true},
		{"pas di tepi kanan-bawah", domain.Annotation{X: 0.75, Y: 0.5, W: 0.25, H: 0.5}, true},
		{"x+w sedikit melebihi 1 dalam toleransi pembulatan", domain.Annotation{X: 0.7, Y: 0, W: 0.3 + eps/2, H: 1}, true},
		{"x mendekati 1", domain.Annotation{X: 0.994, Y: 0, W: 0.006, H: 0.5}, true},

		{"x negatif", domain.Annotation{X: -0.001, Y: 0, W: 0.5, H: 0.5}, false},
		{"y negatif", domain.Annotation{X: 0, Y: -0.001, W: 0.5, H: 0.5}, false},
		{"x = 1", domain.Annotation{X: 1, Y: 0, W: 0.5, H: 0.5}, false},
		{"y = 1", domain.Annotation{X: 0, Y: 1, W: 0.5, H: 0.5}, false},
		{"w = 0", domain.Annotation{X: 0, Y: 0, W: 0, H: 0.5}, false},
		{"h = 0", domain.Annotation{X: 0, Y: 0, W: 0.5, H: 0}, false},
		{"w negatif", domain.Annotation{X: 0.5, Y: 0, W: -0.1, H: 0.5}, false},
		{"w > 1", domain.Annotation{X: 0, Y: 0, W: 1.01, H: 0.5}, false},
		{"h > 1", domain.Annotation{X: 0, Y: 0, W: 0.5, H: 1.01}, false},
		{"di bawah ukuran minimum", domain.Annotation{X: 0.1, Y: 0.1, W: 0.0049, H: 0.5}, false},
		{"tinggi di bawah minimum", domain.Annotation{X: 0.1, Y: 0.1, W: 0.5, H: 0.004}, false},
		{"x+w melebihi 1", domain.Annotation{X: 0.6, Y: 0, W: 0.5, H: 0.5}, false},
		{"y+h melebihi 1", domain.Annotation{X: 0, Y: 0.6, W: 0.5, H: 0.5}, false},
		{"x+w melebihi toleransi", domain.Annotation{X: 0.7, Y: 0, W: 0.3 + 10*eps, H: 1}, false},
		{"NaN", domain.Annotation{X: math.NaN(), Y: 0, W: 0.5, H: 0.5}, false},
		{"Inf", domain.Annotation{X: 0, Y: 0, W: math.Inf(1), H: 0.5}, false},
		{"-Inf", domain.Annotation{X: math.Inf(-1), Y: 0, W: 0.5, H: 0.5}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.a.ValidateBBox()
			if tc.ok && err != nil {
				t.Fatalf("seharusnya valid: %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("seharusnya ditolak")
				}
				if codeOf(t, err) != "INVALID_BBOX" {
					t.Fatalf("code = %s", codeOf(t, err))
				}
			}
		})
	}
}

// ---------------------------------------------------------------- teks anotasi

func TestNormalizeAnnotationText(t *testing.T) {
	const max = 1000
	good := map[string]string{
		"Kerusakan di sisi kiri": "Kerusakan di sisi kiri",
		"  dipangkas  ":          "dipangkas",
		"baris1\nbaris2":         "baris1\nbaris2",
		"baris1\r\nbaris2":       "baris1\nbaris2",
		"\n\nisi\n\n":            "isi",
		"emoji 👨‍👩‍👧 keluarga":   "emoji 👨‍👩‍👧 keluarga", // ZWJ (Cf) harus tetap boleh
		"日本語のテキスト":               "日本語のテキスト",
		"a":                      "a",
		strings.Repeat("x", max): strings.Repeat("x", max),
		strings.Repeat("あ", max): strings.Repeat("あ", max), // dihitung per rune, bukan byte
	}
	for in, want := range good {
		got, err := domain.NormalizeAnnotationText(in, max)
		if err != nil || got != want {
			t.Errorf("Normalize(%.30q) = %.30q, %v; want %.30q", in, got, err, want)
		}
	}

	bad := map[string]string{
		"kosong":               "",
		"hanya spasi":          "   \t  ",
		"hanya newline":        "\n\r\n",
		"terlalu panjang":      strings.Repeat("x", max+1),
		"terlalu panjang rune": strings.Repeat("あ", max+1),
		"null byte":            "ab\x00cd",
		"tab":                  "a\tb",
		"carriage return":      "a\rb",
		"escape":               "a\x1bb",
		"C1 control":           "a\u0085b",
		"UTF-8 rusak":          "a\xffb",
		"RLO bidi override":    "kode‮exe.txt",
		"isolate bidi":         "a⁦b",
		"DEL":                  "a\x7fb",
	}
	for name, in := range bad {
		_, err := domain.NormalizeAnnotationText(in, max)
		if err == nil {
			t.Errorf("%s: seharusnya ditolak (%q)", name, in)
			continue
		}
		if codeOf(t, err) != "INVALID_ANNOTATION_TEXT" {
			t.Errorf("%s: code = %s", name, codeOf(t, err))
		}
	}
}

func TestAnnotationValidateReturnsNormalizedCopy(t *testing.T) {
	orig := domain.Annotation{Text: "  halo \r\n dunia  ", X: 0.1, Y: 0.1, W: 0.2, H: 0.2}
	got, err := orig.Validate(100)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "halo \n dunia" {
		t.Fatalf("teks = %q", got.Text)
	}
	if orig.Text != "  halo \r\n dunia  " {
		t.Fatal("Validate tidak boleh mengubah nilai asli")
	}
	if got.X != orig.X || got.W != orig.W {
		t.Fatal("koordinat harus dipertahankan")
	}
	if _, err := (domain.Annotation{Text: "ok", X: 2, Y: 0, W: 0.1, H: 0.1}).Validate(100); codeOf(t, err) != "INVALID_BBOX" {
		t.Fatalf("bbox salah harus menghasilkan INVALID_BBOX: %v", err)
	}
	if _, err := (domain.Annotation{Text: "", X: 0.1, Y: 0.1, W: 0.1, H: 0.1}).Validate(100); codeOf(t, err) != "INVALID_ANNOTATION_TEXT" {
		t.Fatalf("teks kosong harus menghasilkan INVALID_ANNOTATION_TEXT: %v", err)
	}
}
