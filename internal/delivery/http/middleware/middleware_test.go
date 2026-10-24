package middleware_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/delivery/http/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

func prefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func TestRequestID(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/", func(c *gin.Context) { c.String(200, httpx.RequestID(c)) })

	do := func(inbound string) (hdr, body string) {
		req := httptest.NewRequest("GET", "/", nil)
		if inbound != "" {
			req.Header.Set(middleware.HeaderRequestID, inbound)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Header().Get(middleware.HeaderRequestID), w.Body.String()
	}

	if h, b := do(""); len(h) != 32 || h != b {
		t.Fatalf("id baru harus 32 hex dan sama dengan context: %q %q", h, b)
	}
	if h, _ := do("abc-12345.test_ID"); h != "abc-12345.test_ID" {
		t.Fatalf("id aman dari klien harus dipakai: %q", h)
	}
	for _, bad := range []string{"short", "has space in it!", "a\nb-injected-line", strings.Repeat("x", 65), "ünïcode-id-123"} {
		if h, _ := do(bad); h == bad || len(h) != 32 {
			t.Fatalf("id tidak aman %q harus diganti, dapat %q", bad, h)
		}
	}
}

func TestClientIP(t *testing.T) {
	trusted := prefixes(t, "127.0.0.1/32", "::1/128", "172.16.0.0/12")
	tests := []struct {
		name, remote, cf, want string
	}{
		{"proxy tepercaya + header valid", "172.18.0.4:5555", "203.0.113.9", "203.0.113.9"},
		{"proxy tepercaya + header IPv6", "127.0.0.1:1", "2001:db8::7", "2001:db8::7"},
		{"proxy tepercaya tanpa header", "172.18.0.4:5555", "", "172.18.0.4"},
		{"proxy tepercaya + header sampah", "172.18.0.4:5555", "bukan-ip", "172.18.0.4"},
		{"klien langsung memalsukan header", "198.51.100.20:4444", "203.0.113.9", "198.51.100.20"},
		{"IPv4-mapped IPv6 dari proxy", "[::ffff:172.18.0.4]:80", "203.0.113.9", "203.0.113.9"},
		{"IPv6 langsung", "[2001:db8::1]:80", "203.0.113.9", "2001:db8::1"},
		{"header berspasi", "127.0.0.1:1", " 203.0.113.9 ", "203.0.113.9"},
		{"remote tanpa port", "198.51.100.20", "", "198.51.100.20"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(middleware.ClientIP(trusted))
			r.GET("/", func(c *gin.Context) { c.String(200, httpx.ClientIP(c)) })
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remote
			if tc.cf != "" {
				req.Header.Set("CF-Connecting-IP", tc.cf)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if got := w.Body.String(); got != tc.want {
				t.Fatalf("client ip = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoggerFieldsAndNoSensitiveData(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.ClientIP(prefixes(t, "127.0.0.1/32")), middleware.Logger(log))
	r.GET("/photos/:id/content", func(c *gin.Context) { c.String(200, "data") })
	r.GET("/boom", func(c *gin.Context) { c.Status(503) })

	req := httptest.NewRequest("GET", "/photos/abc/content?exp=1&sig=SUPERSECRETSIG", nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	req.Header.Set("Authorization", "Bearer TOKEN-RAHASIA")
	req.Header.Set("X-Edit-Token", "EDIT-RAHASIA")
	r.ServeHTTP(httptest.NewRecorder(), req)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/boom", nil))

	out := buf.String()
	for _, secret := range []string{"SUPERSECRETSIG", "TOKEN-RAHASIA", "EDIT-RAHASIA", "sig=", "exp="} {
		if strings.Contains(out, secret) {
			t.Fatalf("log membocorkan %q:\n%s", secret, out)
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("harus 2 baris log, dapat %d", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"request_id", "method", "path", "route", "status", "duration_ms", "client_ip", "bytes"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("field %q hilang di log: %v", k, rec)
		}
	}
	if rec["client_ip"] != "203.0.113.9" || rec["route"] != "/photos/:id/content" || rec["path"] != "/photos/abc/content" || rec["level"] != "INFO" {
		t.Errorf("nilai log salah: %v", rec)
	}
	var rec2 map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &rec2)
	if rec2["level"] != "ERROR" {
		t.Errorf("status 5xx harus level ERROR: %v", rec2)
	}
}

func TestRecoveryReturnsJSON500AndKeepsServing(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery(log))
	r.GET("/panic", func(c *gin.Context) { panic("rahasia-internal-xyz") })
	r.GET("/ok", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	if w.Code != 500 {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != "INTERNAL" {
		t.Fatalf("body error salah: %s (%v)", w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), "rahasia-internal-xyz") || strings.Contains(w.Body.String(), "goroutine") {
		t.Fatalf("respons membocorkan detail panic: %s", w.Body.String())
	}
	if !strings.Contains(buf.String(), "rahasia-internal-xyz") || !strings.Contains(buf.String(), "stack") {
		t.Fatalf("panic dan stack harus masuk log: %s", buf.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/ok", nil))
	if w.Code != 200 {
		t.Fatalf("server harus tetap melayani setelah panic, status %d", w.Code)
	}
}

func TestDeadlinesIgnoredOnUnsupportedWriter(t *testing.T) {
	r := gin.New()
	r.Use(middleware.Deadlines(time.Second, time.Second))
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })
	w := httptest.NewRecorder() // tidak mendukung deadline
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestGinWriterSupportsResponseController(t *testing.T) {
	r := gin.New()
	var rcErr error
	r.GET("/", func(c *gin.Context) {
		rcErr = http.NewResponseController(c.Writer).SetReadDeadline(time.Time{})
		c.String(200, "ok")
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if rcErr != nil {
		t.Fatalf("gin.ResponseWriter harus mendukung ResponseController, dapat: %v", rcErr)
	}
}

// Uji fungsional: deadline baca per-route benar-benar memutus body yang datang terlalu lambat.
func TestDeadlinesCutSlowBody(t *testing.T) {
	r := gin.New()
	r.POST("/slow", middleware.Deadlines(150*time.Millisecond, 0), func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.String(http.StatusRequestTimeout, "timeout")
			return
		}
		c.String(200, "ok")
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("POST /slow HTTP/1.1\r\nHost: x\r\nContent-Length: 10\r\n\r\nA")) // 1 dari 10 byte, sisanya tak pernah datang
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("server harus membalas setelah deadline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want 408", resp.StatusCode)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("deadline tidak bekerja, butuh %v", el)
	}
}
