package turnstile_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"photo-annotated/internal/adapter/turnstile"
	"photo-annotated/internal/domain"
)

// fakeVerifier meniru Cloudflare: token "good" lolos tepat sekali (pemakaian ulang ditolak).
type fakeVerifier struct {
	calls atomic.Int32
	delay time.Duration
	mu    sync.Mutex
	used  map[string]bool
}

func (f *fakeVerifier) Verify(ctx context.Context, token, ip string) error {
	f.calls.Add(1)
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.used == nil {
		f.used = map[string]bool{}
	}
	if token != "good" || f.used[token] {
		return domain.Forbidden("TURNSTILE_FAILED", "x")
	}
	f.used[token] = true
	return nil
}

func TestReusableAllowsLimitedUses(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	f := &fakeVerifier{}
	r := turnstile.NewReusableWithClock(f, 3, 5*time.Minute, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if err := r.Verify(context.Background(), "good", "1.2.3.4"); err != nil {
			t.Fatalf("pemakaian %d: %v", i+1, err)
		}
	}
	if err := r.Verify(context.Background(), "good", "1.2.3.4"); code(err) != "TURNSTILE_FAILED" {
		t.Fatalf("pemakaian ke-4: %v", err)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("siteverify dipanggil %d kali", f.calls.Load())
	}
}

func TestReusableBoundToIPAndTTL(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	f := &fakeVerifier{}
	r := turnstile.NewReusableWithClock(f, 10, 5*time.Minute, func() time.Time { return now })
	if err := r.Verify(context.Background(), "good", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(context.Background(), "good", "5.6.7.8"); code(err) != "TURNSTILE_FAILED" {
		t.Fatalf("IP lain harus ditolak: %v", err)
	}
	now = now.Add(5 * time.Minute)
	if err := r.Verify(context.Background(), "good", "1.2.3.4"); code(err) != "TURNSTILE_FAILED" {
		t.Fatalf("setelah TTL harus ditolak: %v", err)
	}
}

func TestReusableConcurrentFirstUseVerifiesOnce(t *testing.T) {
	f := &fakeVerifier{delay: 50 * time.Millisecond}
	r := turnstile.NewReusable(f, 6, time.Minute)
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.Verify(context.Background(), "good", "1.2.3.4") == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if f.calls.Load() != 1 || ok.Load() != 6 {
		t.Fatalf("calls = %d, lolos = %d; ingin 1 dan 6", f.calls.Load(), ok.Load())
	}
}

func TestReusableDoesNotCacheFailures(t *testing.T) {
	f := &fakeVerifier{}
	r := turnstile.NewReusable(f, 6, time.Minute)
	for i := 0; i < 2; i++ {
		if err := r.Verify(context.Background(), "bad", "1.2.3.4"); code(err) != "TURNSTILE_FAILED" {
			t.Fatalf("err = %v", err)
		}
	}
	if f.calls.Load() != 2 {
		t.Fatalf("kegagalan tidak boleh disimpan: calls = %d", f.calls.Load())
	}
}
