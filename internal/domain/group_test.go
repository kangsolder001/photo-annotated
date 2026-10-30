package domain_test

import (
	"strings"
	"testing"

	"photo-annotated/internal/domain"
)

func TestNormalizeGroupName(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"Kelompok 4 – CL-05", "Kelompok 4 – CL-05", true},
		{"  Kelompok 1  ", "Kelompok 1", true},
		{strings.Repeat("a", 100), strings.Repeat("a", 100), true},
		{strings.Repeat("a", 101), "", false},
		{"", "", false},
		{"  ", "", false},
		{"dua\nbaris", "", false},
		{"bidi‮evil", "", false},
	}
	for _, tc := range tests {
		got, err := domain.NormalizeGroupName(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("%q: got %q err=%v", tc.in, got, err)
		}
		if err != nil && errCode(err) != "INVALID_GROUP_NAME" {
			t.Errorf("%q: kode %v", tc.in, err)
		}
	}
}
