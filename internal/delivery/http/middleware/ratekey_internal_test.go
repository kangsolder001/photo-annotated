package middleware

import "testing"

func TestRateKeyGroupsIPv6By64(t *testing.T) {
	a := rateKey("2001:db8:1:2:aaaa:bbbb:cccc:dddd")
	b := rateKey("2001:db8:1:2:1111:2222:3333:4444")
	c := rateKey("2001:db8:1:3::1")
	if a != b || a == c {
		t.Fatalf("pengelompokan /64 salah: %s %s %s", a, b, c)
	}
	if rateKey("203.0.113.9") != "203.0.113.9" || rateKey("::ffff:203.0.113.9") != "203.0.113.9" {
		t.Fatal("IPv4 (termasuk mapped) harus apa adanya")
	}
	if rateKey("") != unknownKey || rateKey("bukan-ip") != unknownKey {
		t.Fatal("IP tak valid harus ke kunci 'unknown'")
	}
}
