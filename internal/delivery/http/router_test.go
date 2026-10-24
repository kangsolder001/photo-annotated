package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	httpapi "photo-annotated/internal/delivery/http"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newTestRouter(t *testing.T) (http.Handler, *bytes.Buffer) {
	return newTestRouterDB(t, fakeDB{})
}

// baseDeps mengisi semua dependensi router dengan fake; test mengubah yang perlu.
func baseDeps(buf *bytes.Buffer) httpapi.Deps {
	return httpapi.Deps{
		Logger:            slog.New(slog.NewJSONHandler(buf, nil)),
		TrustedProxies:    []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
		DB:                fakeDB{},
		AllowedOrigins:    []string{"https://app.example.com"},
		Presigner:         &fakePresigner{},
		PresignPerMin:     1000,
		Uploader:          &fakeUploader{},
		Submitter:         &fakeSubmitter{},
		Reader:            &fakeReader{},
		SubmissionsPerMin: 1000,
		MaxUploadBytes:    2 << 20,
		UploadTimeout:     60 * time.Second,
	}
}

func newTestRouterDB(t *testing.T, db fakeDB) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	d := baseDeps(&buf)
	d.DB = db
	return httpapi.NewRouter(d), &buf
}

func TestReadyz(t *testing.T) {
	h, _ := newTestRouterDB(t, fakeDB{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ready"`) {
		t.Fatalf("readyz sehat: %d %s", w.Code, w.Body.String())
	}

	h, buf := newTestRouterDB(t, fakeDB{err: errors.New("dial tcp 10.0.0.9:5432: password=hunter2 refused")})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "NOT_READY") {
		t.Fatalf("readyz DB mati: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hunter2") || strings.Contains(w.Body.String(), "10.0.0.9") {
		t.Fatalf("respons membocorkan detail error DB: %s", w.Body.String())
	}
	if !strings.Contains(buf.String(), `"status":503`) || !strings.Contains(buf.String(), "10.0.0.9") {
		t.Fatalf("503 dan penyebabnya harus tercatat di log (bukan di respons): %s", buf.String())
	}
}

func TestRouterAnswersUploadPreflight(t *testing.T) {
	h, _ := newTestRouter(t)
	do := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("OPTIONS", "/api/v1/uploads/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "PUT")
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	w := do("https://app.example.com")
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("preflight upload dari origin sah: %d %v", w.Code, w.Header())
	}
	if w := do("https://evil.example.com"); w.Code != 403 {
		t.Fatalf("origin lain harus ditolak: %d", w.Code)
	}
}

func TestNewRouterRequiresDeps(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewRouter tanpa Deps wajib harus panic (bug wiring)")
		}
	}()
	httpapi.NewRouter(httpapi.Deps{})
}

func TestHealthz(t *testing.T) {
	h, _ := newTestRouter(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("healthz: %d %s", w.Code, w.Body.String())
	}
	if len(w.Header().Get("X-Request-ID")) == 0 {
		t.Fatal("X-Request-ID harus ada di setiap respons")
	}
}

func TestUnknownRouteAndMethodAreJSON(t *testing.T) {
	h, _ := newTestRouter(t)
	for _, tc := range []struct {
		method, path, code string
		status             int
	}{
		{"GET", "/tidak-ada", "NOT_FOUND", 404},
		{"POST", "/healthz", "METHOD_NOT_ALLOWED", 405},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		var b struct {
			Error struct{ Code, Message string }
		}
		if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &b) != nil || b.Error.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q", ct)
		}
	}
}

func TestRequestLoggedWithClientIPFromTrustedProxy(t *testing.T) {
	h, buf := newTestRouter(t)
	req := httptest.NewRequest("GET", "/healthz?sig=RAHASIA", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	h.ServeHTTP(httptest.NewRecorder(), req)
	out := buf.String()
	if !strings.Contains(out, `"client_ip":"203.0.113.9"`) || strings.Contains(out, "RAHASIA") {
		t.Fatalf("log tidak sesuai: %s", out)
	}
}

func TestNewServerHasNoGlobalReadWriteTimeout(t *testing.T) {
	h, _ := newTestRouter(t)
	s := httpapi.NewServer(":0", h)
	if s.ReadTimeout != 0 || s.WriteTimeout != 0 {
		t.Fatalf("ReadTimeout/WriteTimeout global harus 0 (deadline per-route), dapat %v/%v", s.ReadTimeout, s.WriteTimeout)
	}
	if s.ReadHeaderTimeout != 10*time.Second || s.IdleTimeout != 60*time.Second {
		t.Fatalf("timeout server salah: %v %v", s.ReadHeaderTimeout, s.IdleTimeout)
	}
}

// httpapi_NewRouter menghindari impor ganda di file test lain.
func httpapi_NewRouter(d httpapi.Deps) http.Handler { return httpapi.NewRouter(d) }
