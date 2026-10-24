package domain_test

import (
	"strings"
	"testing"

	"photo-annotated/internal/domain"
)

func TestNormalizeLabelName(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"Retak", "Retak", true},
		{"  Penyok samping  ", "Penyok samping", true},
		{strings.Repeat("é", 50), strings.Repeat("é", 50), true},
		{strings.Repeat("a", 51), "", false},
		{"", "", false},
		{"   ", "", false},
		{"dua\nbaris", "", false},
		{"tab\there", "", false},
		{"bidi‮evil", "", false},
		{"\xff", "", false},
	}
	for _, tc := range tests {
		got, err := domain.NormalizeLabelName(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("%q: got %q err=%v", tc.in, got, err)
		}
		if err != nil && errCode(err) != "INVALID_LABEL_NAME" {
			t.Errorf("%q: kode %v", tc.in, err)
		}
	}
}

func TestNormalizeLabelDescriptionAndColor(t *testing.T) {
	if d, err := domain.NormalizeLabelDescription("   "); err != nil || d != "" {
		t.Fatalf("deskripsi kosong: %q %v", d, err)
	}
	if d, err := domain.NormalizeLabelDescription(" Retakan pada dinding paket "); err != nil || d != "Retakan pada dinding paket" {
		t.Fatalf("deskripsi: %q %v", d, err)
	}
	for _, bad := range []string{strings.Repeat("x", 201), "a⁦b", "baris\nbaru"} {
		if _, err := domain.NormalizeLabelDescription(bad); errCode(err) != "INVALID_LABEL_DESCRIPTION" {
			t.Errorf("%q: %v", bad, err)
		}
	}
	for in, want := range map[string]string{"#e4572e": "#e4572e", " #E4572E ": "#e4572e"} {
		if got, err := domain.NormalizeLabelColor(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "e4572e", "#e4572", "#e4572eff", "#g4572e", "red"} {
		if _, err := domain.NormalizeLabelColor(bad); errCode(err) != "INVALID_LABEL_COLOR" {
			t.Errorf("%q: %v", bad, err)
		}
	}
	for _, c := range domain.LabelPalette {
		if got, err := domain.NormalizeLabelColor(c); err != nil || got != c {
			t.Errorf("palet %q tidak valid", c)
		}
	}
}

func TestNormalizeReason(t *testing.T) {
	if r, err := domain.NormalizeReason("  Kotak tidak pas ", "INVALID_REASON"); err != nil || r != "Kotak tidak pas" {
		t.Fatalf("%q %v", r, err)
	}
	for _, bad := range []string{"", " ", strings.Repeat("x", 501), "a\x00b"} {
		if _, err := domain.NormalizeReason(bad, "INVALID_REASON"); errCode(err) != "INVALID_REASON" {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func errCode(err error) string {
	if de, ok := domain.AsError(err); ok {
		return de.Code
	}
	return ""
}
