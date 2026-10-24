package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	httpapi "photo-annotated/internal/delivery/http"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type fakePresigner struct {
	mu  sync.Mutex
	in  []usecase.PresignInput
	out *usecase.PresignOutput
	err error
}

func (f *fakePresigner) Presign(_ context.Context, in usecase.PresignInput) (*usecase.PresignOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, in)
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	return &usecase.PresignOutput{
		PhotoID: uuid.MustParse("0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11"), Method: "PUT",
		URL:       "https://api.example.com/api/v1/uploads/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11?exp=1&sig=x",
		Headers:   map[string]string{"Content-Type": in.ContentType},
		ExpiresAt: time.Date(2026, 10, 24, 12, 10, 0, 0, time.FixedZone("WIB", 7*3600)),
	}, nil
}

func routerWith(t *testing.T, p *fakePresigner, perMin int) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	d := baseDeps(&buf)
	d.Presigner, d.PresignPerMin = p, perMin
	return httpapi.NewRouter(d), &buf
}

func postPresign(h http.Handler, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/uploads/presign", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const goodBody = `{"content_type":"image/webp","size_bytes":412345,"width":1920,"height":1440}`

func TestPresignHandlerSuccess(t *testing.T) {
	fp := &fakePresigner{}
	h, _ := routerWith(t, fp, 100)
	w := postPresign(h, goodBody, "application/json")
	if w.Code != 201 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		PhotoID string `json:"photo_id"`
		Upload  struct {
			Method    string            `json:"method"`
			URL       string            `json:"url"`
			Headers   map[string]string `json:"headers"`
			ExpiresAt string            `json:"expires_at"`
		} `json:"upload"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.PhotoID != "0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11" || got.Upload.Method != "PUT" ||
		got.Upload.Headers["Content-Type"] != "image/webp" || !strings.Contains(got.Upload.URL, "/api/v1/uploads/") {
		t.Fatalf("body salah: %s", w.Body.String())
	}
	// waktu harus UTC ber-"Z" apa pun zona waktu sumbernya (spec §5.1)
	if got.Upload.ExpiresAt != "2026-10-24T05:10:00Z" {
		t.Fatalf("expires_at = %q", got.Upload.ExpiresAt)
	}
	// input diteruskan apa adanya ke usecase
	if len(fp.in) != 1 || fp.in[0].ContentType != "image/webp" || fp.in[0].SizeBytes != 412345 ||
		fp.in[0].Width != 1920 || fp.in[0].Height != 1440 || fp.in[0].OwnerID != nil {
		t.Fatalf("input usecase: %+v", fp.in)
	}
}

func TestPresignHandlerRequestErrors(t *testing.T) {
	h, _ := routerWith(t, &fakePresigner{}, 100)
	big := `{"content_type":"` + strings.Repeat("x", 70<<10) + `"}`
	for _, tc := range []struct {
		name, body, ct string
		status         int
		code           string
	}{
		{"JSON rusak", `{"content_type":`, "application/json", 400, "INVALID_JSON"},
		{"field tak dikenal", `{"content_type":"image/webp","size_bytes":1,"width":1,"height":1,"owner_id":"x"}`, "application/json", 400, "INVALID_JSON"},
		{"tipe field salah", `{"content_type":"image/webp","size_bytes":"besar","width":1,"height":1}`, "application/json", 400, "INVALID_JSON"},
		{"content-type request salah", goodBody, "text/plain", 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"tanpa content-type", goodBody, "", 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"body > 64 KB", big, "application/json", 413, "PAYLOAD_TOO_LARGE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := postPresign(h, tc.body, tc.ct)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s; want %d %s", w.Code, w.Body.String(), tc.status, tc.code)
			}
		})
	}
	// metode lain
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/uploads/presign", nil))
	if w.Code != 405 {
		t.Fatalf("GET presign = %d", w.Code)
	}
}

func TestPresignHandlerMapsUsecaseErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validasi", domain.Invalid("INVALID_CONTENT_TYPE", "content_type must be image/webp or image/jpeg"), 422, "INVALID_CONTENT_TYPE"},
		{"penyimpanan penuh", domain.Unavailable("STORAGE_FULL", "photo storage is full"), 503, "STORAGE_FULL"},
		{"dibungkus", errors.Join(errors.New("konteks"), domain.Unavailable("STORAGE_UNAVAILABLE", "photo storage is unavailable")), 503, "STORAGE_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := routerWith(t, &fakePresigner{err: tc.err}, 100)
			w := postPresign(h, goodBody, "application/json")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPresignHandlerHidesInternalErrors(t *testing.T) {
	h, buf := routerWith(t, &fakePresigner{err: errors.New("pq: password authentication failed for user photo_annotated (10.0.0.9)")}, 100)
	w := postPresign(h, goodBody, "application/json")
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"INTERNAL"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "10.0.0.9") {
		t.Fatalf("respons membocorkan detail internal: %s", w.Body.String())
	}
	if !strings.Contains(buf.String(), "password authentication failed") {
		t.Fatalf("penyebab harus ada di log: %s", buf.String())
	}
}

func TestPresignHandlerIsRateLimited(t *testing.T) {
	h, _ := routerWith(t, &fakePresigner{}, 3)
	hit := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/uploads/presign", strings.NewReader(goodBody))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1"
		req.Header.Set("CF-Connecting-IP", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	for i := 0; i < 3; i++ {
		if w := hit("203.0.113.1"); w.Code != 201 {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := hit("203.0.113.1")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "RATE_LIMITED") {
		t.Fatalf("request ke-4: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if w := hit("203.0.113.2"); w.Code != 201 {
		t.Fatalf("IP lain tidak boleh terkena: %d", w.Code)
	}
}

func TestPresignHandlerHasCORSHeaders(t *testing.T) {
	h, _ := routerWith(t, &fakePresigner{}, 100)
	req := httptest.NewRequest("POST", "/api/v1/uploads/presign", strings.NewReader(goodBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("header CORS hilang: %v", w.Header())
	}
}
