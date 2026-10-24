package httpapi_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	httpapi "photo-annotated/internal/delivery/http"
)

func newTestRouter(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return httpapi.NewRouter(httpapi.Deps{
		Logger:         slog.New(slog.NewJSONHandler(&buf, nil)),
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
	}), &buf
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
