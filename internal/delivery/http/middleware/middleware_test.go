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

func TestLoggerIncludesInternalError(t *testing.T) {
	var buf bytes.Buffer
	r := gin.New()
	r.Use(middleware.Logger(slog.New(slog.NewJSONHandler(&buf, nil))))
	r.GET("/x", func(c *gin.Context) {
		_ = c.Error(io.ErrUnexpectedEOF)
		c.Status(503)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !strings.Contains(buf.String(), "unexpected EOF") || !strings.Contains(buf.String(), `"error"`) {
		t.Fatalf("error internal harus ada di log: %s", buf.String())
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

// ---------------------------------------------------------------- rate limit (via Gin)

func TestRateLimitMiddleware(t *testing.T) {
	r := gin.New()
	r.Use(middleware.ClientIP(prefixes(t, "127.0.0.1/32")))
	r.GET("/x", middleware.RateLimit(middleware.NewRateLimiter(3)), func(c *gin.Context) { c.String(200, "ok") })

	hit := func(cf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = "127.0.0.1:1"
		req.Header.Set("CF-Connecting-IP", cf)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for i := 0; i < 3; i++ {
		if w := hit("203.0.113.1"); w.Code != 200 {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := hit("203.0.113.1")
	if w.Code != 429 {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra == "" || ra == "0" {
		t.Fatalf("Retry-After = %q", ra)
	}
	if !strings.Contains(w.Body.String(), "RATE_LIMITED") {
		t.Fatalf("body: %s", w.Body.String())
	}
	if w := hit("203.0.113.2"); w.Code != 200 {
		t.Fatalf("IP lain tidak boleh terkena limit: %d", w.Code)
	}
}

// ---------------------------------------------------------------- CORS

func corsEngine() *gin.Engine {
	r := gin.New()
	r.Use(middleware.CORS([]string{"https://app.example.com"}))
	r.PUT("/api/v1/uploads/:id", func(c *gin.Context) { c.Status(204) })
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })
	return r
}

func preflight(r http.Handler, origin, method, headers, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("OPTIONS", path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Header.Set("Access-Control-Request-Method", method)
	if headers != "" {
		req.Header.Set("Access-Control-Request-Headers", headers)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCORSPreflightAllowed(t *testing.T) {
	r := corsEngine()
	w := preflight(r, "https://app.example.com", "PUT", "content-type, X-Edit-Token", "/api/v1/uploads/abc")
	if w.Code != 204 {
		t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
	}
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
		h.Get("Access-Control-Allow-Methods") != "GET, POST, PUT, OPTIONS" ||
		h.Get("Access-Control-Allow-Headers") != "Content-Type, Authorization, X-Edit-Token, X-Turnstile-Token" ||
		h.Get("Access-Control-Max-Age") != "3600" {
		t.Fatalf("header preflight salah: %v", h)
	}
	if !strings.Contains(strings.Join(h.Values("Vary"), ","), "Origin") {
		t.Fatalf("Vary: Origin wajib: %v", h.Values("Vary"))
	}
}

func TestCORSPreflightRejections(t *testing.T) {
	r := corsEngine()
	for name, w := range map[string]*httptest.ResponseRecorder{
		"origin lain":        preflight(r, "https://evil.example.com", "PUT", "content-type", "/api/v1/uploads/abc"),
		"origin null":        preflight(r, "null", "PUT", "", "/api/v1/uploads/abc"),
		"method DELETE":      preflight(r, "https://app.example.com", "DELETE", "", "/x"),
		"header tak dikenal": preflight(r, "https://app.example.com", "PUT", "content-type, x-evil", "/x"),
		"skema beda":         preflight(r, "http://app.example.com", "PUT", "", "/x"),
	} {
		if w.Code != 403 {
			t.Errorf("%s: status = %d, want 403", name, w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: tidak boleh ada ACAO pada penolakan", name)
		}
	}
}

func TestCORSPreflightWorksForUnknownRoute(t *testing.T) {
	// preflight harus dijawab sebelum routing, jadi rute yang tak ada/tak mendukung OPTIONS pun lolos
	w := preflight(corsEngine(), "https://app.example.com", "POST", "content-type", "/api/v1/belum-ada")
	if w.Code != 204 {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestCORSActualRequestHeaders(t *testing.T) {
	r := corsEngine()
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("origin diizinkan: %d %v", w.Code, w.Header())
	}
	if !strings.Contains(w.Header().Get("Access-Control-Expose-Headers"), "Retry-After") {
		t.Fatalf("Retry-After harus bisa dibaca FE: %v", w.Header())
	}

	req = httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("origin lain tidak boleh mendapat ACAO: %v", w.Header())
	}
	if w.Code != 200 {
		t.Fatalf("request biasa tidak diblokir server (CORS bukan otorisasi): %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil)) // tanpa Origin
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "" {
		t.Fatalf("tanpa Origin tidak ada header CORS: %v", w.Header())
	}
}

// ---------------------------------------------------------------- body limit + DecodeJSON

type payload struct {
	Name string `json:"name"`
}

func bodyEngine(limit int64) *gin.Engine {
	r := gin.New()
	r.POST("/j", middleware.BodyLimit(limit), func(c *gin.Context) {
		var p payload
		if !httpx.DecodeJSON(c, &p) {
			return
		}
		c.String(200, p.Name)
	})
	return r
}

func postJSON(r http.Handler, ct string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/j", body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBodyLimitAndDecodeJSON(t *testing.T) {
	r := bodyEngine(64)
	tests := []struct {
		name string
		ct   string
		body io.Reader
		want int
		code string
	}{
		{"ok", "application/json", strings.NewReader(`{"name":"budi"}`), 200, ""},
		{"ok dengan charset", "application/json; charset=utf-8", strings.NewReader(`{"name":"budi"}`), 200, ""},
		{"content-type salah", "text/plain", strings.NewReader(`{"name":"budi"}`), 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"tanpa content-type", "", strings.NewReader(`{"name":"budi"}`), 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"JSON rusak", "application/json", strings.NewReader(`{"name":`), 400, "INVALID_JSON"},
		{"field tak dikenal", "application/json", strings.NewReader(`{"name":"a","admin":true}`), 400, "INVALID_JSON"},
		{"data setelah objek", "application/json", strings.NewReader(`{"name":"a"}{"name":"b"}`), 400, "INVALID_JSON"},
		{"tipe salah", "application/json", strings.NewReader(`{"name":123}`), 400, "INVALID_JSON"},
		{"terlalu besar (Content-Length)", "application/json", strings.NewReader(`{"name":"` + strings.Repeat("x", 100) + `"}`), 413, "PAYLOAD_TOO_LARGE"},
		{"terlalu besar (chunked)", "application/json", io.MultiReader(strings.NewReader(`{"name":"` + strings.Repeat("x", 100) + `"}`)), 413, "PAYLOAD_TOO_LARGE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := postJSON(r, tc.ct, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
			if tc.code != "" && !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("body %s tidak memuat %s", w.Body.String(), tc.code)
			}
		})
	}
}

func TestBodyLimitRejectsEarlyByContentLength(t *testing.T) {
	read := false
	r := gin.New()
	r.POST("/j", middleware.BodyLimit(10), func(c *gin.Context) { read = true; c.Status(200) })
	req := httptest.NewRequest("POST", "/j", strings.NewReader(strings.Repeat("x", 100)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 413 || read {
		t.Fatalf("harus 413 sebelum handler berjalan: code=%d handlerJalan=%v", w.Code, read)
	}
}
