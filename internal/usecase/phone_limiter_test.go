package usecase_test

import (
	"testing"
	"time"

	"photo-annotated/internal/usecase"
)

func TestPhoneLimiter(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	l := usecase.NewPhoneLimiterWithClock(time.Minute, 3, clock)
	const a = "6281234567890"

	if ok, _ := l.Allow(a); !ok {
		t.Fatal("permintaan pertama harus lolos")
	}
	ok, wait := l.Allow(a)
	if ok || wait != time.Minute {
		t.Fatalf("cooldown: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("6281234567891"); !ok {
		t.Fatal("nomor lain tidak terpengaruh")
	}
	now = now.Add(time.Minute)
	if ok, _ := l.Allow(a); !ok {
		t.Fatal("setelah cooldown harus lolos")
	}
	now = now.Add(time.Minute)
	if ok, _ := l.Allow(a); !ok {
		t.Fatal("permintaan ke-3 dalam sejam harus lolos")
	}
	now = now.Add(time.Minute)
	ok, wait = l.Allow(a)
	if ok || wait != 57*time.Minute {
		t.Fatalf("batas per jam: ok=%v wait=%v (ingin 57m)", ok, wait)
	}
	// ditolak karena batas per jam tidak menahan cooldown: tetap ditolak per jam, bukan per menit
	now = now.Add(57 * time.Minute)
	if ok, _ := l.Allow(a); !ok {
		t.Fatal("permintaan tertua sudah keluar dari jendela jam: harus lolos")
	}
}

func TestPhoneLimiterRefund(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	l := usecase.NewPhoneLimiterWithClock(time.Minute, 2, func() time.Time { return now })
	const a = "6281234567890"
	l.Allow(a)
	l.Refund(a) // pengiriman gagal: boleh langsung mencoba lagi
	if ok, _ := l.Allow(a); !ok {
		t.Fatal("setelah refund cooldown tidak berlaku")
	}
	l.Refund(a)
	now = now.Add(time.Second)
	l.Allow(a)
	now = now.Add(time.Minute)
	l.Allow(a)
	now = now.Add(time.Minute)
	if ok, _ := l.Allow(a); ok {
		t.Fatal("refund tidak boleh menambah jatah melebihi batas per jam")
	}
}
