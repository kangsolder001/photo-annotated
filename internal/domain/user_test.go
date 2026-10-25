package domain

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	for _, tc := range []struct {
		pw string
		ok bool
	}{
		{"rahasia123", true},
		{"12345678", true},
		{"kata sandi panjang dengan spasi", true},
		{"pässwörd", true}, // 8 karakter walau > 8 byte
		{"pendek", false},
		{"        ", false},
		{"rahasia\x00123", false},
		{strings.Repeat("a", 72), true},
		{strings.Repeat("a", 73), false},
		{string([]byte{0xff, 0xfe, 'a', 'b', 'c', 'd', 'e', 'f'}), false},
	} {
		err := ValidatePassword(tc.pw)
		if (err == nil) != tc.ok {
			t.Errorf("%q: err=%v, ingin ok=%v", tc.pw, err, tc.ok)
		}
	}
}
