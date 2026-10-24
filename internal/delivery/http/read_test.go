package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

type memFile struct {
	*bytes.Reader
	info usecase.FileInfo
}

func (memFile) Close() error             { return nil }
func (m memFile) Info() usecase.FileInfo { return m.info }
func newMemFile(b []byte, mod time.Time) memFile {
	return memFile{Reader: bytes.NewReader(b), info: usecase.FileInfo{Size: int64(len(b)), ModTime: mod}}
}

type fakeReader struct {
	mu      sync.Mutex
	callers []usecase.Caller
	ids     []uuid.UUID
	view    *usecase.SubmissionView
	url     *usecase.PhotoURL
	content *usecase.PhotoContentResult
	err     error
	lastExp int64
	lastSig string
}

func (f *fakeReader) Submission(_ context.Context, c usecase.Caller, id uuid.UUID) (*usecase.SubmissionView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callers, f.ids = append(f.callers, c), append(f.ids, id)
	if f.err != nil {
		return nil, f.err
	}
	return f.view, nil
}

func (f *fakeReader) PhotoURL(_ context.Context, c usecase.Caller, id uuid.UUID) (*usecase.PhotoURL, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callers, f.ids = append(f.callers, c), append(f.ids, id)
	if f.err != nil {
		return nil, f.err
	}
	return f.url, nil
}

func (f *fakeReader) PhotoContent(_ context.Context, id uuid.UUID, exp int64, sig string) (*usecase.PhotoContentResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids, f.lastExp, f.lastSig = append(f.ids, id), exp, sig
	if f.err != nil {
		return nil, f.err
	}
	return f.content, nil
}

func routerWithReader(t *testing.T, fr *fakeReader) http.Handler {
	t.Helper()
	var buf bytes.Buffer
	d := baseDeps(&buf)
	d.Reader = fr
	return httpapi.NewRouter(d)
}

func sampleView() *usecase.SubmissionView {
	sub := uuid.MustParse("7d2a1c2e-6a5e-7c55-9c0b-2f6c2d8e9a11")
	owner := uuid.New()
	created := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	return &usecase.SubmissionView{
		Detail: domain.SubmissionDetail{
			Submission: domain.Submission{ID: sub, OwnerID: &owner, Version: 2, CreatedAt: created, UpdatedAt: created.Add(time.Hour)},
			Photos: []domain.PhotoWithAnnotations{{
				Photo: domain.Photo{ID: uuid.MustParse(photoA), Position: 0, ContentType: "image/webp", SizeBytes: 412345,
					Width: 1920, Height: 1440, StorageKey: "2026/10/24/rahasia.webp", SHA256: bytes.Repeat([]byte{1}, 32), OwnerID: &owner},
				Annotations: []domain.Annotation{{Text: "kotak 1", X: 0.12, Y: 0.3, W: 0.25, H: 0.18}},
			}, {
				Photo:       domain.Photo{ID: uuid.MustParse(photoB), Position: 1, ContentType: "image/jpeg", SizeBytes: 1000, Width: 10, Height: 10},
				Annotations: nil,
			}},
		},
		EditableUntil: created.Add(24 * time.Hour), Editable: true,
	}
}

func get(h http.Handler, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------- GET /submissions/{id}

func TestGetSubmissionHandler(t *testing.T) {
	fr := &fakeReader{view: sampleView()}
	h := routerWithReader(t, fr)
	w := get(h, "/api/v1/submissions/7d2a1c2e-6a5e-7c55-9c0b-2f6c2d8e9a11", map[string]string{"X-Edit-Token": "tok-xyz"})
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if fr.callers[0].EditToken != "tok-xyz" || fr.ids[0].String() != "7d2a1c2e-6a5e-7c55-9c0b-2f6c2d8e9a11" {
		t.Fatalf("caller/id: %+v %v", fr.callers, fr.ids)
	}
	// field internal tidak boleh keluar
	for _, secret := range []string{"storage_key", "rahasia.webp", "sha256", "owner", "client_ip", "client_request_id"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("respons membocorkan %q: %s", secret, w.Body.String())
		}
	}
	var got struct {
		ID            string `json:"id"`
		Version       int    `json:"version"`
		CreatedAt     string `json:"created_at"`
		UpdatedAt     string `json:"updated_at"`
		EditableUntil string `json:"editable_until"`
		Editable      bool   `json:"editable"`
		Photos        []struct {
			PhotoID     string `json:"photo_id"`
			Position    int    `json:"position"`
			ContentType string `json:"content_type"`
			SizeBytes   int64  `json:"size_bytes"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			Annotations []struct {
				Text       string  `json:"text"`
				X, Y, W, H float64 `json:"-"`
			} `json:"annotations"`
		} `json:"photos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "7d2a1c2e-6a5e-7c55-9c0b-2f6c2d8e9a11" || got.Version != 2 || !got.Editable ||
		got.CreatedAt != "2026-10-24T12:00:00Z" || got.UpdatedAt != "2026-10-24T13:00:00Z" || got.EditableUntil != "2026-10-25T12:00:00Z" {
		t.Fatalf("body: %s", w.Body.String())
	}
	if len(got.Photos) != 2 || got.Photos[0].PhotoID != photoA || got.Photos[0].Width != 1920 || got.Photos[1].Position != 1 {
		t.Fatalf("foto: %s", w.Body.String())
	}
	if len(got.Photos[0].Annotations) != 1 || got.Photos[0].Annotations[0].Text != "kotak 1" {
		t.Fatalf("anotasi: %s", w.Body.String())
	}
	// foto tanpa anotasi: [] (bukan null) agar klien tidak perlu menangani null
	if !strings.Contains(w.Body.String(), `"annotations":[]`) {
		t.Fatalf("anotasi kosong harus []: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"x":0.12`) {
		t.Fatalf("koordinat hilang: %s", w.Body.String())
	}
}

func TestReadHandlersNotFoundAndErrors(t *testing.T) {
	for _, path := range []string{"/api/v1/submissions/bukan-uuid", "/api/v1/photos/bukan-uuid/url", "/api/v1/photos/bukan-uuid/content?exp=1&sig=x"} {
		fr := &fakeReader{view: sampleView()}
		w := get(routerWithReader(t, fr), path, nil)
		if w.Code != 404 || !strings.Contains(w.Body.String(), "NOT_FOUND") || len(fr.ids) != 0 {
			t.Errorf("%s: %d %s (usecase dipanggil: %d)", path, w.Code, w.Body.String(), len(fr.ids))
		}
	}
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", domain.ErrNotFound, 404, "NOT_FOUND"},
		{"tanda tangan", domain.Forbidden("INVALID_SIGNATURE", "x"), 403, "INVALID_SIGNATURE"},
		{"kedaluwarsa", domain.Gone("URL_EXPIRED", "x"), 410, "URL_EXPIRED"},
		{"file hilang", domain.NotFound("PHOTO_FILE_MISSING", "x"), 404, "PHOTO_FILE_MISSING"},
		{"internal", errors.New("pgx: password=hunter2"), 500, "INTERNAL"},
	} {
		for _, path := range []string{
			"/api/v1/submissions/" + photoA, "/api/v1/photos/" + photoA + "/url", "/api/v1/photos/" + photoA + "/content?exp=1&sig=x",
		} {
			w := get(routerWithReader(t, &fakeReader{err: tc.err}), path, nil)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"`+tc.code+`"`) || strings.Contains(w.Body.String(), "hunter2") {
				t.Errorf("%s %s: %d %s", tc.name, path, w.Code, w.Body.String())
			}
		}
	}
}

// ---------------------------------------------------------------- GET /photos/{id}/url

func TestPhotoURLHandler(t *testing.T) {
	exp := time.Date(2026, 10, 24, 12, 5, 0, 0, time.FixedZone("WIB", 7*3600))
	fr := &fakeReader{url: &usecase.PhotoURL{URL: "https://api.example.com/api/v1/photos/" + photoA + "/content?exp=1&sig=s", ExpiresAt: exp}}
	w := get(routerWithReader(t, fr), "/api/v1/photos/"+photoA+"/url", map[string]string{"X-Edit-Token": "tok"})
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["url"] == "" || got["expires_at"] != "2026-10-24T05:05:00Z" {
		t.Fatalf("body: %s", w.Body.String())
	}
	if fr.callers[0].EditToken != "tok" || fr.ids[0].String() != photoA {
		t.Fatalf("caller/id: %+v %v", fr.callers, fr.ids)
	}
}

// ---------------------------------------------------------------- GET /photos/{id}/content

func contentReader(data []byte) *fakeReader {
	return &fakeReader{content: &usecase.PhotoContentResult{
		File:        newMemFile(data, time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)),
		ContentType: "image/webp",
		ETag:        `"abc123"`,
	}}
}

func TestPhotoContentHandlerHeadersAndBody(t *testing.T) {
	data := []byte("0123456789abcdefghij")
	fr := contentReader(data)
	w := get(routerWithReader(t, fr), "/api/v1/photos/"+photoA+"/content?exp=1792806473&sig=abc_DEF", nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	h := w.Header()
	for k, want := range map[string]string{
		"Content-Type": "image/webp", "X-Content-Type-Options": "nosniff", "Content-Disposition": "inline",
		"Cache-Control": "private, max-age=300", "ETag": `"abc123"`, "Accept-Ranges": "bytes", "Content-Length": "20",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if h.Get("Last-Modified") == "" {
		t.Error("Last-Modified harus ada")
	}
	if fr.ids[0].String() != photoA || fr.lastExp != 1792806473 || fr.lastSig != "abc_DEF" {
		t.Fatalf("parameter ke usecase: %v %d %q", fr.ids, fr.lastExp, fr.lastSig)
	}
}

func TestPhotoContentHandlerRangeAndConditional(t *testing.T) {
	data := []byte("0123456789abcdefghij")
	// Range → 206
	w := get(routerWithReader(t, contentReader(data)), "/api/v1/photos/"+photoA+"/content?exp=1&sig=s", map[string]string{"Range": "bytes=5-9"})
	if w.Code != 206 || w.Body.String() != "56789" || w.Header().Get("Content-Range") != "bytes 5-9/20" {
		t.Fatalf("range: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	// Range di luar batas → 416
	w = get(routerWithReader(t, contentReader(data)), "/api/v1/photos/"+photoA+"/content?exp=1&sig=s", map[string]string{"Range": "bytes=100-200"})
	if w.Code != 416 {
		t.Fatalf("range di luar batas: %d", w.Code)
	}
	// If-None-Match cocok → 304 tanpa body
	w = get(routerWithReader(t, contentReader(data)), "/api/v1/photos/"+photoA+"/content?exp=1&sig=s", map[string]string{"If-None-Match": `"abc123"`})
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatalf("conditional: %d %q", w.Code, w.Body.String())
	}
	// If-None-Match tidak cocok → 200
	w = get(routerWithReader(t, contentReader(data)), "/api/v1/photos/"+photoA+"/content?exp=1&sig=s", map[string]string{"If-None-Match": `"lain"`})
	if w.Code != 200 {
		t.Fatalf("etag beda: %d", w.Code)
	}
}

func TestPhotoContentHandlerBadExpAndNoSigStillGoToUsecase(t *testing.T) {
	for _, q := range []string{"", "?exp=abc&sig=x", "?exp=99999999999999999999&sig=x", "?sig=x"} {
		fr := &fakeReader{err: domain.Forbidden("INVALID_SIGNATURE", "x")}
		w := get(routerWithReader(t, fr), "/api/v1/photos/"+photoA+"/content"+q, nil)
		if w.Code != 403 || fr.lastExp != 0 {
			t.Errorf("query %q: %d exp=%d (exp cacat harus 0)", q, w.Code, fr.lastExp)
		}
	}
}

type closeCounter struct {
	memFile
	closed *int
}

func (c closeCounter) Close() error { *c.closed++; return nil }

func TestPhotoContentHandlerClosesFile(t *testing.T) {
	n := 0
	fr := &fakeReader{content: &usecase.PhotoContentResult{
		File: closeCounter{newMemFile([]byte("abc"), time.Now()), &n}, ContentType: "image/jpeg",
	}}
	w := get(routerWithReader(t, fr), "/api/v1/photos/"+photoA+"/content?exp=1&sig=s", nil)
	if w.Code != 200 || n != 1 {
		t.Fatalf("status=%d closed=%d (file harus ditutup tepat sekali)", w.Code, n)
	}
	if w.Header().Get("ETag") != "" {
		t.Fatalf("tanpa ETag dari usecase, header ETag tidak boleh ada: %q", w.Header().Get("ETag"))
	}
	_ = io.Discard
}
