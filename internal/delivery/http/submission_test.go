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

type fakeSubmitter struct {
	mu  sync.Mutex
	in  []usecase.SubmitInput
	out *usecase.SubmitOutput
	err error
}

func (f *fakeSubmitter) Submit(_ context.Context, in usecase.SubmitInput) (*usecase.SubmitOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, in)
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	return &usecase.SubmitOutput{
		SubmissionID: uuid.MustParse("7d2a1c2e-6a5e-7c55-9c0b-2f6c2d8e9a11"), Version: 1,
		CreatedAt: time.Date(2026, 10, 24, 12, 5, 12, 0, time.FixedZone("WIB", 7*3600)), EditToken: "tok-abc",
	}, nil
}

func routerWithSubmitter(t *testing.T, fs *fakeSubmitter, perMin int) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	d := baseDeps(&buf)
	d.Submitter, d.SubmissionsPerMin = fs, perMin
	return httpapi.NewRouter(d), &buf
}

func postSubmission(h http.Handler, body, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/submissions", strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const (
	photoA  = "0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11"
	photoB  = "11111111-2222-4333-8444-555555555555"
	reqID   = "c1f7e0a4-1111-4222-8333-444444444444"
	goodSub = `{"photos":[{"photo_id":"` + photoA + `","annotations":[` +
		`{"label_id":3,"x":0.12,"y":0.30,"w":0.25,"h":0.18},` +
		`{"label_id":4,"x":0.55,"y":0.10,"w":0.30,"h":0.12}]},` +
		`{"photo_id":"` + photoB + `"}],"client_request_id":"` + reqID + `"}`
)

func TestSubmissionHandlerCreated(t *testing.T) {
	fs := &fakeSubmitter{}
	h, _ := routerWithSubmitter(t, fs, 100)
	w := postSubmission(h, goodSub, "application/json")
	if w.Code != 201 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["submission_id"] != "7d2a1c2e-6a5e-7c55-9c0b-2f6c2d8e9a11" || got["version"] != float64(1) ||
		got["created_at"] != "2026-10-24T05:05:12Z" || got["edit_token"] != "tok-abc" {
		t.Fatalf("body: %s", w.Body.String())
	}

	if len(fs.in) != 1 {
		t.Fatalf("usecase dipanggil %d kali", len(fs.in))
	}
	in := fs.in[0]
	if in.ClientRequestID.String() != reqID || len(in.Photos) != 2 || in.OwnerID != nil {
		t.Fatalf("input: %+v", in)
	}
	if in.Photos[0].PhotoID.String() != photoA || len(in.Photos[0].Annotations) != 2 ||
		in.Photos[0].Annotations[0] != (domain.Annotation{LabelID: 3, X: 0.12, Y: 0.30, W: 0.25, H: 0.18}) {
		t.Fatalf("anotasi foto 1: %+v", in.Photos[0])
	}
	if in.Photos[1].PhotoID.String() != photoB || in.Photos[1].Annotations == nil || len(in.Photos[1].Annotations) != 0 {
		t.Fatalf("foto tanpa anotasi harus slice kosong: %#v", in.Photos[1])
	}
	if in.ClientIP == nil || in.ClientIP.String() != "203.0.113.9" {
		t.Fatalf("client ip = %v (harus dari CF-Connecting-IP proxy tepercaya)", in.ClientIP)
	}
}

func TestSubmissionHandlerReplayIs200AndTokenIsOptional(t *testing.T) {
	out := &usecase.SubmitOutput{SubmissionID: uuid.New(), Version: 3, CreatedAt: time.Now(), Replayed: true}
	h, _ := routerWithSubmitter(t, &fakeSubmitter{out: out}, 100)
	w := postSubmission(h, goodSub, "application/json")
	if w.Code != 200 {
		t.Fatalf("replay harus 200, dapat %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "edit_token") {
		t.Fatalf("edit_token harus dihilangkan bila kosong: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"version":3`) {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestSubmissionHandlerBadRequests(t *testing.T) {
	fs := &fakeSubmitter{}
	h, _ := routerWithSubmitter(t, fs, 100)
	big := `{"photos":[{"photo_id":"` + photoA + `","annotations":[{"text":"` + strings.Repeat("x", 70<<10) + `"}]}],"client_request_id":"` + reqID + `"}`
	for _, tc := range []struct {
		name, body, ct string
		status         int
		code           string
	}{
		{"JSON rusak", `{"photos":`, "application/json", 400, "INVALID_JSON"},
		{"field tak dikenal", `{"photos":[],"client_request_id":"` + reqID + `","owner_id":"x"}`, "application/json", 400, "INVALID_JSON"},
		{"field tak dikenal di foto", `{"photos":[{"photo_id":"` + photoA + `","status":"attached"}],"client_request_id":"` + reqID + `"}`, "application/json", 400, "INVALID_JSON"},
		{"photo_id bukan UUID", `{"photos":[{"photo_id":"abc"}],"client_request_id":"` + reqID + `"}`, "application/json", 400, "INVALID_JSON"},
		{"client_request_id bukan UUID", `{"photos":[],"client_request_id":"123"}`, "application/json", 400, "INVALID_JSON"},
		{"koordinat bukan angka", `{"photos":[{"photo_id":"` + photoA + `","annotations":[{"label_id":1,"x":"0.1","y":0,"w":1,"h":1}]}],"client_request_id":"` + reqID + `"}`, "application/json", 400, "INVALID_JSON"},
		{"content-type salah", goodSub, "text/plain", 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"body > 64 KB", big, "application/json", 413, "PAYLOAD_TOO_LARGE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := postSubmission(h, tc.body, tc.ct)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s; want %d %s", w.Code, w.Body.String(), tc.status, tc.code)
			}
		})
	}
	if len(fs.in) != 0 {
		t.Fatalf("usecase tidak boleh dipanggil untuk request yang cacat: %d panggilan", len(fs.in))
	}
}

func TestSubmissionHandlerPassesThroughEmptyShapes(t *testing.T) {
	// photos kosong / hilang bukan urusan handler: usecase yang menolak dengan kode yang tepat
	fs := &fakeSubmitter{err: domain.Invalid("INVALID_PHOTO_COUNT", "a submission needs between 1 and 5 photos")}
	h, _ := routerWithSubmitter(t, fs, 100)
	for _, body := range []string{`{"client_request_id":"` + reqID + `"}`, `{"photos":[],"client_request_id":"` + reqID + `"}`, `{"photos":null,"client_request_id":"` + reqID + `"}`} {
		w := postSubmission(h, body, "application/json")
		if w.Code != 422 || !strings.Contains(w.Body.String(), "INVALID_PHOTO_COUNT") {
			t.Fatalf("%s → %d %s", body, w.Code, w.Body.String())
		}
	}
}

func TestSubmissionHandlerMapsUsecaseErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"jumlah foto", domain.Invalid("INVALID_PHOTO_COUNT", "x"), 422, "INVALID_PHOTO_COUNT"},
		{"bbox", domain.Invalid("INVALID_BBOX", "photos[0].annotations[1]: x"), 422, "INVALID_BBOX"},
		{"foto tidak ada", domain.Invalid("PHOTO_NOT_FOUND", "x"), 422, "PHOTO_NOT_FOUND"},
		{"belum ter-upload", domain.Invalid("PHOTO_NOT_UPLOADED", "x"), 422, "PHOTO_NOT_UPLOADED"},
		{"file bermasalah", domain.Invalid("PHOTO_FILE_INVALID", "x"), 422, "PHOTO_FILE_INVALID"},
		{"foto tidak tersedia", domain.Conflict("PHOTO_NOT_AVAILABLE", "x"), 409, "PHOTO_NOT_AVAILABLE"},
		{"duplikat request", domain.Conflict("DUPLICATE_CLIENT_REQUEST", "x"), 409, "DUPLICATE_CLIENT_REQUEST"},
		{"SSD bermasalah", domain.Unavailable("STORAGE_UNAVAILABLE", "x"), 503, "STORAGE_UNAVAILABLE"},
		{"internal", errors.New("pgx: password=hunter2 refused"), 500, "INTERNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := routerWithSubmitter(t, &fakeSubmitter{err: tc.err}, 100)
			w := postSubmission(h, goodSub, "application/json")
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "hunter2") {
				t.Fatalf("detail internal bocor: %s", w.Body.String())
			}
		})
	}
}

func TestSubmissionHandlerIsRateLimited(t *testing.T) {
	h, _ := routerWithSubmitter(t, &fakeSubmitter{}, 10)
	for i := 0; i < 10; i++ {
		if w := postSubmission(h, goodSub, "application/json"); w.Code != 201 {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := postSubmission(h, goodSub, "application/json")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "RATE_LIMITED") {
		t.Fatalf("request ke-11: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
}

func TestSubmissionRouteOnlyAcceptsPOST(t *testing.T) {
	h, _ := routerWithSubmitter(t, &fakeSubmitter{}, 100)
	for _, m := range []string{"GET", "DELETE", "PATCH"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(m, "/api/v1/submissions", nil))
		if w.Code != 405 && w.Code != 404 {
			t.Errorf("%s → %d", m, w.Code)
		}
	}
}
