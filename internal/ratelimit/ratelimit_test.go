package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)} }

func TestRateLimiterSlidingWindow(t *testing.T) {
	clk := newClock()
	l := NewWithOptions(3, time.Minute, 1000, clk.Now)

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d seharusnya lolos", i+1)
		}
		clk.Advance(10 * time.Second) // t=0,10,20
	}
	// sekarang t=30s; request ke-4 ditolak dan harus menunggu sampai catatan t=0 keluar (60s-30s)
	ok, retry := l.Allow("a")
	if ok || retry != 30*time.Second {
		t.Fatalf("ok=%v retry=%v, want false/30s", ok, retry)
	}
	// penolakan tidak boleh menggeser jendela
	clk.Advance(29 * time.Second)
	if ok, retry := l.Allow("a"); ok || retry != time.Second {
		t.Fatalf("masih harus ditolak, retry=%v", retry)
	}
	clk.Advance(time.Second) // t=60s: catatan t=0 baru keluar
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("setelah Retry-After berlalu request harus lolos")
	}
	// hanya 1 slot yang dibebaskan (t=0); catatan t=10 dan 20 masih di jendela
	if ok, retry := l.Allow("a"); ok || retry != 10*time.Second {
		t.Fatalf("slot kedua belum bebas: ok=%v retry=%v", ok, retry)
	}
}

func TestRateLimiterKeysAreIndependent(t *testing.T) {
	clk := newClock()
	l := NewWithOptions(1, time.Minute, 1000, clk.Now)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal()
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a harus ditolak")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b tidak boleh terpengaruh a")
	}
}

func TestRateLimiterSweepFreesMemory(t *testing.T) {
	clk := newClock()
	l := NewWithOptions(5, time.Minute, 1000, clk.Now)
	for i := 0; i < 200; i++ {
		l.Allow(fmt.Sprintf("ip-%d", i))
	}
	if l.Len() != 200 {
		t.Fatalf("Len = %d", l.Len())
	}
	clk.Advance(2 * time.Minute)
	l.Allow("baru") // memicu sweep
	if l.Len() != 1 {
		t.Fatalf("setelah sweep Len = %d, want 1", l.Len())
	}
}

func TestRateLimiterBoundsKeyCount(t *testing.T) {
	clk := newClock()
	l := NewWithOptions(5, time.Minute, 50, clk.Now)
	for i := 0; i < 5000; i++ {
		l.Allow(fmt.Sprintf("ip-%d", i))
	}
	if n := l.Len(); n > 50 {
		t.Fatalf("jumlah kunci %d melebihi batas 50 (memori bisa dihabiskan)", n)
	}
}

func TestRateLimiterConcurrentExactlyLimit(t *testing.T) {
	l := New(30, time.Minute)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("same"); ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 30 {
		t.Fatalf("lolos %d, want tepat 30", allowed.Load())
	}
}

func TestRateLimiterPanicsOnInvalidLimit(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("limit 0 harus panic")
		}
	}()
	New(0, time.Minute)
}

func TestLimiterUndoReturnsOneSlot(t *testing.T) {
	clk := newClock()
	l := NewWithOptions(2, time.Minute, 100, clk.Now)
	l.Allow("a")
	clk.Advance(10 * time.Second)
	l.Allow("a")
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("jatah habis")
	}
	l.Undo("a") // membuang kejadian TERBARU (t=10s), bukan yang tertua
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("setelah Undo satu jatah harus kembali")
	}
	// kejadian tertua (t=0) tetap ada: jendela tetap bergeser dari t=0
	clk.Advance(50 * time.Second) // t=60s: t=0 keluar dari jendela
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("kejadian tertua harus sudah keluar dari jendela")
	}
	// Undo pada kunci tanpa catatan aman dan tidak membuat kunci
	l.Undo("tidak-ada")
	if l.Len() != 1 {
		t.Fatalf("Len = %d", l.Len())
	}
	// Undo kejadian terakhir menghapus kunci
	l2 := NewWithOptions(1, time.Minute, 100, clk.Now)
	l2.Allow("x")
	l2.Undo("x")
	if l2.Len() != 0 {
		t.Fatalf("kunci harus dihapus: Len = %d", l2.Len())
	}
}
