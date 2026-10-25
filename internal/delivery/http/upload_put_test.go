package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type fakeUploader struct {
	mu   sync.Mutex
	in   []usecase.UploadInput
	body [][]byte
	err  error
}

func (f *fakeUploader) Upload(_ context.Context, in usecase.UploadInput) error {
	b, _ := io.ReadAll(in.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, in)
	f.body = append(f.body, b)
	return f.err
}

func routerWithUploader(t *testing.T, fu *fakeUploader, maxBytes int64) http.Handler {
	t.Helper()
	var buf bytes.Buffer
	d := baseDeps(&buf)
	d.Uploader, d.MaxUploadBytes = fu, maxBytes
	return httpapi_NewRouter(d)
}

const putID = "0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11"

func put(h http.Handler, path string, body io.Reader, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", path, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestPutHandlerPassesParsedInputToUsecase(t *testing.T) {
	fu := &fakeUploader{}
	h := routerWithUploader(t, fu, 1<<20)
	w := put(h, "/api/v1/uploads/"+putID+"?exp=1792806473&sig=abc_DEF-123", strings.NewReader("ISI-FOTO"), "image/webp")
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if len(fu.in) != 1 {
		t.Fatalf("usecase dipanggil %d kali", len(fu.in))
	}
	in := fu.in[0]
	if in.PhotoID != uuid.MustParse(putID) || in.Exp != 1792806473 || in.Sig != "abc_DEF-123" ||
		in.ContentType != "image/webp" || in.ContentLength != 8 || string(fu.body[0]) != "ISI-FOTO" {
		t.Fatalf("input salah: %+v body=%q", in, fu.body[0])
	}
}

func TestPutHandlerBadParameters(t *testing.T) {
	fu := &fakeUploader{err: domain.Forbidden("INVALID_SIGNATURE", "upload URL is not valid")}
	h := routerWithUploader(t, fu, 1<<20)

	// id bukan UUID → 404, usecase tidak dipanggil
	if w := put(h, "/api/v1/uploads/bukan-uuid?exp=1&sig=x", strings.NewReader("x"), "image/webp"); w.Code != 404 {
		t.Fatalf("id salah: %d", w.Code)
	}
	if len(fu.in) != 0 {
		t.Fatal("usecase tidak boleh dipanggil untuk id yang bukan UUID")
	}
	// exp/sig hilang atau cacat diteruskan sebagai nilai kosong → usecase menolak dengan 403
	for _, q := range []string{"", "?exp=1", "?sig=x", "?exp=abc&sig=x", "?exp=99999999999999999999&sig=x"} {
		w := put(h, "/api/v1/uploads/"+putID+q, strings.NewReader("x"), "image/webp")
		if w.Code != 403 || !strings.Contains(w.Body.String(), "INVALID_SIGNATURE") {
			t.Fatalf("query %q: %d %s", q, w.Code, w.Body.String())
		}
	}
	last := fu.in[len(fu.in)-1]
	if last.Exp != 0 {
		t.Fatalf("exp cacat harus 0 agar ditolak, dapat %d", last.Exp)
	}
}

func TestPutHandlerRejectsOversizeBeforeCallingUsecase(t *testing.T) {
	fu := &fakeUploader{}
	h := routerWithUploader(t, fu, 1000)
	w := put(h, "/api/v1/uploads/"+putID+"?exp=1&sig=x", bytes.NewReader(make([]byte, 1001)), "image/webp")
	if w.Code != 413 || !strings.Contains(w.Body.String(), "PAYLOAD_TOO_LARGE") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(fu.in) != 0 {
		t.Fatal("usecase tidak boleh dipanggil untuk body yang jelas kebesaran")
	}
	// tepat batas masih diteruskan
	if w := put(h, "/api/v1/uploads/"+putID+"?exp=1&sig=x", bytes.NewReader(make([]byte, 1000)), "image/webp"); w.Code != 204 {
		t.Fatalf("tepat batas: %d", w.Code)
	}
}

func TestPutRouteIsNotSubjectToJSONBodyLimit(t *testing.T) {
	fu := &fakeUploader{}
	h := routerWithUploader(t, fu, 1<<20)
	big := make([]byte, 300<<10) // 300 KB > 64 KB batas rute JSON
	if w := put(h, "/api/v1/uploads/"+putID+"?exp=1&sig=x", bytes.NewReader(big), "image/webp"); w.Code != 204 {
		t.Fatalf("status = %d", w.Code)
	}
	if len(fu.body[0]) != len(big) {
		t.Fatalf("usecase menerima %d byte, want %d", len(fu.body[0]), len(big))
	}
}

func TestPutHandlerPassesUnknownLengthAsNegative(t *testing.T) {
	fu := &fakeUploader{}
	h := routerWithUploader(t, fu, 1<<20)
	w := put(h, "/api/v1/uploads/"+putID+"?exp=1&sig=x", io.MultiReader(strings.NewReader("chunked")), "image/webp")
	if w.Code != 204 || fu.in[0].ContentLength != -1 {
		t.Fatalf("status=%d length=%d (usecase yang menolak bila < 0)", w.Code, fu.in[0].ContentLength)
	}
}

func TestPutHandlerMapsUsecaseErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
		retry  string
	}{
		{"tanda tangan", domain.Forbidden("INVALID_SIGNATURE", "x"), 403, "INVALID_SIGNATURE", ""},
		{"kedaluwarsa", domain.Gone("URL_EXPIRED", "x"), 410, "URL_EXPIRED", ""},
		{"bukan pending", domain.Conflict("PHOTO_NOT_AVAILABLE", "x"), 409, "PHOTO_NOT_AVAILABLE", ""},
		{"sedang berjalan", domain.Conflict("UPLOAD_IN_PROGRESS", "x").WithRetryAfter(2 * time.Second), 409, "UPLOAD_IN_PROGRESS", "2"},
		{"slot penuh", domain.Unavailable("UPLOAD_BUSY", "x").WithRetryAfter(2 * time.Second), 503, "UPLOAD_BUSY", "2"},
		{"tanpa Content-Length", domain.LengthRequired("LENGTH_REQUIRED", "x"), 411, "LENGTH_REQUIRED", ""},
		{"ukuran tidak cocok", domain.Invalid("SIZE_MISMATCH", "x"), 422, "SIZE_MISMATCH", ""},
		{"gambar tidak valid", domain.Invalid("INVALID_IMAGE", "x"), 422, "INVALID_IMAGE", ""},
		{"klien lambat", domain.Timeout("UPLOAD_TIMEOUT", "x"), 408, "UPLOAD_TIMEOUT", ""},
		{"SSD penuh", domain.Unavailable("STORAGE_FULL", "x"), 503, "STORAGE_FULL", ""},
		{"internal", errors.New("pgx: connection refused 10.0.0.9"), 500, "INTERNAL", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := routerWithUploader(t, &fakeUploader{err: tc.err}, 1<<20)
			w := put(h, "/api/v1/uploads/"+putID+"?exp=1&sig=x", strings.NewReader("x"), "image/webp")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Retry-After"); got != tc.retry {
				t.Fatalf("Retry-After = %q, want %q", got, tc.retry)
			}
			if strings.Contains(w.Body.String(), "10.0.0.9") {
				t.Fatalf("detail internal bocor: %s", w.Body.String())
			}
		})
	}
}

func TestPutRouteOnlyAcceptsPUT(t *testing.T) {
	h := routerWithUploader(t, &fakeUploader{}, 1<<20)
	for _, m := range []string{"GET", "POST", "DELETE", "PATCH"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(m, "/api/v1/uploads/"+putID+"?exp=1&sig=x", nil))
		if w.Code != 405 {
			t.Errorf("%s → %d, want 405", m, w.Code)
		}
	}
}

func TestPutPreflightAllowsContentTypeHeader(t *testing.T) {
	h := routerWithUploader(t, &fakeUploader{}, 1<<20)
	req := httptest.NewRequest("OPTIONS", "/api/v1/uploads/"+putID, nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("preflight upload: %d", w.Code)
	}
}
