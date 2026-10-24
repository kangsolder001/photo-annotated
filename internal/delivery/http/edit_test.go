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

type fakeEditor struct {
	mu  sync.Mutex
	in  []usecase.EditInput
	out *usecase.EditOutput
	err error
}

func (f *fakeEditor) Edit(_ context.Context, in usecase.EditInput) (*usecase.EditOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, in)
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	return &usecase.EditOutput{
		SubmissionID: uuid.MustParse(subID), Version: 3,
		UpdatedAt: time.Date(2026, 10, 24, 13, 0, 0, 0, time.FixedZone("WIB", 7*3600)),
	}, nil
}

const subID = "7d2a1c2e-6a5e-7c55-9c0b-2f6c2d8e9a11"

func routerWithEditor(t *testing.T, fe *fakeEditor, perMin int) http.Handler {
	t.Helper()
	var buf bytes.Buffer
	d := baseDeps(&buf)
	d.Editor, d.SubmissionsPerMin = fe, perMin
	return httpapi.NewRouter(d)
}

func putSubmission(h http.Handler, id, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", "/api/v1/submissions/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Edit-Token", token)
	}
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const goodEdit = `{"version":2,"photos":[{"photo_id":"` + photoA + `","annotations":[` +
	`{"text":"diperbarui","x":0.1,"y":0.2,"w":0.3,"h":0.4}]},{"photo_id":"` + photoB + `"}]}`

func TestEditHandlerSuccess(t *testing.T) {
	fe := &fakeEditor{}
	w := putSubmission(routerWithEditor(t, fe, 100), subID, goodEdit, "tok-xyz")
	if w.Code != 200 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["submission_id"] != subID || got["version"] != float64(3) || got["updated_at"] != "2026-10-24T06:00:00Z" {
		t.Fatalf("body: %s", w.Body.String())
	}
	if len(got) != 3 {
		t.Fatalf("hanya submission_id, version, updated_at yang boleh keluar: %s", w.Body.String())
	}
	in := fe.in[0]
	if in.SubmissionID.String() != subID || in.Caller.EditToken != "tok-xyz" || in.Version != 2 || len(in.Photos) != 2 {
		t.Fatalf("input: %+v", in)
	}
	if in.Photos[0].PhotoID.String() != photoA || len(in.Photos[0].Annotations) != 1 ||
		in.Photos[0].Annotations[0] != (domain.Annotation{Text: "diperbarui", X: 0.1, Y: 0.2, W: 0.3, H: 0.4}) {
		t.Fatalf("foto 1: %+v", in.Photos[0])
	}
	if in.Photos[1].Annotations == nil || len(in.Photos[1].Annotations) != 0 {
		t.Fatalf("foto tanpa anotasi harus slice kosong: %#v", in.Photos[1].Annotations)
	}
}

func TestEditHandlerBadRequests(t *testing.T) {
	fe := &fakeEditor{}
	h := routerWithEditor(t, fe, 100)
	big := `{"version":1,"photos":[{"photo_id":"` + photoA + `","annotations":[{"text":"` + strings.Repeat("x", 70<<10) + `"}]}]}`
	for _, tc := range []struct {
		name, id, body string
		status         int
		code           string
	}{
		{"id bukan UUID", "abc", goodEdit, 404, "NOT_FOUND"},
		{"JSON rusak", subID, `{"version":`, 400, "INVALID_JSON"},
		{"client_request_id tidak berlaku pada edit", subID, `{"version":1,"photos":[],"client_request_id":"` + reqID + `"}`, 400, "INVALID_JSON"},
		{"field tak dikenal", subID, `{"version":1,"photos":[],"owner":"x"}`, 400, "INVALID_JSON"},
		{"version bukan angka", subID, `{"version":"2","photos":[]}`, 400, "INVALID_JSON"},
		{"photo_id bukan UUID", subID, `{"version":1,"photos":[{"photo_id":"x"}]}`, 400, "INVALID_JSON"},
		{"body > 64 KB", subID, big, 413, "PAYLOAD_TOO_LARGE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := putSubmission(h, tc.id, tc.body, "t")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s; want %d %s", w.Code, w.Body.String(), tc.status, tc.code)
			}
		})
	}
	if len(fe.in) != 0 {
		t.Fatalf("usecase tidak boleh dipanggil untuk request cacat: %d", len(fe.in))
	}
	// Content-Type salah
	req := httptest.NewRequest("PUT", "/api/v1/submissions/"+subID, strings.NewReader(goodEdit))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 415 {
		t.Fatalf("content-type salah: %d", w.Code)
	}
}

func TestEditHandlerMissingVersionIsZero(t *testing.T) {
	// version hilang → 0 → usecase menolak INVALID_VERSION (handler tidak menebak default)
	fe := &fakeEditor{err: domain.Invalid("INVALID_VERSION", "version must be >= 1")}
	w := putSubmission(routerWithEditor(t, fe, 100), subID, `{"photos":[]}`, "t")
	if w.Code != 422 || fe.in[0].Version != 0 {
		t.Fatalf("status=%d version=%d", w.Code, fe.in[0].Version)
	}
}

func TestEditHandlerMapsUsecaseErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"tidak ada / tanpa hak", domain.ErrNotFound, 404, "NOT_FOUND"},
		{"admin", domain.Forbidden("EDIT_NOT_ALLOWED", "x"), 403, "EDIT_NOT_ALLOWED"},
		{"jendela tertutup", domain.Forbidden("EDIT_WINDOW_CLOSED", "x"), 403, "EDIT_WINDOW_CLOSED"},
		{"versi basi", domain.Conflict("VERSION_CONFLICT", "x"), 409, "VERSION_CONFLICT"},
		{"foto tidak tersedia", domain.Conflict("PHOTO_NOT_AVAILABLE", "x"), 409, "PHOTO_NOT_AVAILABLE"},
		{"validasi", domain.Invalid("INVALID_PHOTO_COUNT", "x"), 422, "INVALID_PHOTO_COUNT"},
		{"versi tidak valid", domain.Invalid("INVALID_VERSION", "x"), 422, "INVALID_VERSION"},
		{"internal", errors.New("pgx: password=hunter2"), 500, "INTERNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := putSubmission(routerWithEditor(t, &fakeEditor{err: tc.err}, 100), subID, goodEdit, "t")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"`+tc.code+`"`) || strings.Contains(w.Body.String(), "hunter2") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// POST dan PUT berbagi satu limiter per IP (spec §9.4: "submissions (POST & PUT): 10 / menit").
func TestSubmissionsLimiterIsSharedBetweenPostAndPut(t *testing.T) {
	var buf bytes.Buffer
	d := baseDeps(&buf)
	d.SubmissionsPerMin = 4
	h := httpapi.NewRouter(d)
	for i := 0; i < 2; i++ {
		if w := postSubmission(h, goodSub, "application/json"); w.Code != 201 {
			t.Fatalf("POST %d: %d", i+1, w.Code)
		}
		if w := putSubmission(h, subID, goodEdit, "t"); w.Code != 200 {
			t.Fatalf("PUT %d: %d", i+1, w.Code)
		}
	}
	// kuota 4 habis oleh 2 POST + 2 PUT: keduanya kini ditolak
	if w := postSubmission(h, goodSub, "application/json"); w.Code != 429 {
		t.Fatalf("POST ke-5: %d", w.Code)
	}
	w := putSubmission(h, subID, goodEdit, "t")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("PUT ke-5: %d %v", w.Code, w.Header())
	}
}
