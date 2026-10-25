package turnstile_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"photo-annotated/internal/adapter/turnstile"
	"photo-annotated/internal/domain"
)

const secret = "turnstile-secret"

type fakeCF struct {
	srv   *httptest.Server
	calls atomic.Int32
	last  atomic.Pointer[http.Request]
	form  atomic.Value // url.Values
}

func newFakeCF(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) *fakeCF {
	f := &fakeCF{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		_ = r.ParseForm()
		f.form.Store(r.PostForm)
		f.last.Store(r)
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func code(err error) string {
	if de, ok := domain.AsError(err); ok {
		return de.Code
	}
	return ""
}

func TestVerify(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) {
		if r.PostForm.Get("response") == "good" {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}
	cases := []struct {
		name, token string
		handler     func(http.ResponseWriter, *http.Request)
		want        string // "" = lolos
		wantCalls   int32
	}{
		{"token sah", "good", ok, "", 1},
		{"token ditolak", "bad", ok, "TURNSTILE_FAILED", 1},
		{"token kosong tanpa memanggil cloudflare", "  ", ok, "TURNSTILE_FAILED", 0},
		{"token terlalu panjang", strings.Repeat("a", 2049), ok, "TURNSTILE_FAILED", 0},
		{"cloudflare 500", "good", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, "TURNSTILE_UNAVAILABLE", 1},
		{"badan bukan json", "good", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) }, "TURNSTILE_UNAVAILABLE", 1},
		{"redirect tidak diikuti", "good", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://example.com", 302) }, "TURNSTILE_UNAVAILABLE", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cf := newFakeCF(t, tc.handler)
			v, err := turnstile.New(secret, cf.srv.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = v.Verify(context.Background(), tc.token, "203.0.113.9")
			if got := code(err); got != tc.want {
				t.Fatalf("code = %q (err %v), ingin %q", got, err, tc.want)
			}
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if c := cf.calls.Load(); c != tc.wantCalls {
				t.Fatalf("panggilan = %d, ingin %d", c, tc.wantCalls)
			}
			if tc.wantCalls > 0 {
				f := cf.form.Load().(interface{ Get(string) string })
				if f.Get("secret") != secret || f.Get("remoteip") != "203.0.113.9" {
					t.Fatalf("form tidak lengkap: secret=%q remoteip=%q", f.Get("secret"), f.Get("remoteip"))
				}
			}
		})
	}
}

func TestVerifyUnreachableFailsClosedWithoutLeakingURL(t *testing.T) {
	cf := newFakeCF(t, func(w http.ResponseWriter, r *http.Request) {})
	url := cf.srv.URL
	cf.srv.Close()
	v, _ := turnstile.New(secret, url, time.Second)
	err := v.Verify(context.Background(), "good", "")
	if code(err) != "TURNSTILE_UNAVAILABLE" {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("secret bocor di error")
	}
}

func TestVerifyTimeout(t *testing.T) {
	cf := newFakeCF(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	v, _ := turnstile.New(secret, cf.srv.URL, 50*time.Millisecond)
	if err := v.Verify(context.Background(), "good", ""); code(err) != "TURNSTILE_UNAVAILABLE" {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyCanceledContext(t *testing.T) {
	cf := newFakeCF(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	v, _ := turnstile.New(secret, cf.srv.URL, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := v.Verify(ctx, "good", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := turnstile.New(" ", "", time.Second); err == nil {
		t.Fatal("secret kosong harus ditolak")
	}
	if _, err := turnstile.New(secret, "", 0); err == nil {
		t.Fatal("timeout 0 harus ditolak")
	}
}
