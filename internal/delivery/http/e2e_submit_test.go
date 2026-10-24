package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/imgfix"
)

type submitResp struct {
	Status       int
	SubmissionID uuid.UUID
	Version      int
	EditToken    string
	Code         string
	Message      string
	Raw          string
}

func (s *stack) submit(t testing.TB, body string) submitResp {
	t.Helper()
	resp, err := s.client.Post(s.srv.URL+"/api/v1/submissions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := submitResp{Status: resp.StatusCode, Raw: string(raw)}
	var ok struct {
		SubmissionID uuid.UUID `json:"submission_id"`
		Version      int       `json:"version"`
		EditToken    string    `json:"edit_token"`
		Error        struct{ Code, Message string }
	}
	if json.Unmarshal(raw, &ok) == nil {
		out.SubmissionID, out.Version, out.EditToken = ok.SubmissionID, ok.Version, ok.EditToken
		out.Code, out.Message = ok.Error.Code, ok.Error.Message
	}
	return out
}

// uploadedPhoto menjalankan presign + PUT sungguhan dan mengembalikan id foto.
func (s *stack) uploadedPhoto(t testing.TB, fill byte) uuid.UUID {
	t.Helper()
	body := imgfix.WebP(1920, 1440, 8000, fill)
	pr := s.presign(t, "image/webp", len(body), 1920, 1440)
	if r := s.putBytes(t, pr.URL, "image/webp", body); r.Status != 204 {
		t.Fatalf("PUT status %d (%s)", r.Status, r.Code)
	}
	return pr.ID
}

func submitBody(reqID uuid.UUID, photos ...uuid.UUID) string {
	var ps []string
	for i, p := range photos {
		ps = append(ps, `{"photo_id":"`+p.String()+`","annotations":[`+
			`{"text":"foto `+string(rune('A'+i))+` kotak 1","x":0.12,"y":0.30,"w":0.25,"h":0.18},`+
			`{"text":"baris1\nbaris2","x":0.55,"y":0.10,"w":0.30,"h":0.12}]}`)
	}
	return `{"photos":[` + strings.Join(ps, ",") + `],"client_request_id":"` + reqID.String() + `"}`
}

func TestE2ESubmitFullFlow(t *testing.T) {
	st := newStack(t, stackOpts{})
	a, b := st.uploadedPhoto(t, 0x11), st.uploadedPhoto(t, 0x22)
	reqID := uuid.New()

	res := st.submit(t, submitBody(reqID, a, b))
	if res.Status != 201 {
		t.Fatalf("status %d: %s", res.Status, res.Raw)
	}
	if res.Version != 1 || res.SubmissionID.Version() != 7 {
		t.Fatalf("respons: %+v", res)
	}
	et, _ := signer.NewEditTokens(e2eEditSecret)
	if res.EditToken == "" || !et.Verify(res.SubmissionID, res.EditToken) {
		t.Fatalf("edit_token tidak sah: %q", res.EditToken)
	}

	// semuanya tersimpan: submission, foto attached berurutan, anotasi lengkap
	d, err := st.subs.GetDetail(context.Background(), res.SubmissionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Photos) != 2 || d.Photos[0].Photo.ID != a || d.Photos[1].Photo.ID != b ||
		d.Photos[0].Photo.Status != domain.PhotoAttached || len(d.Photos[0].Annotations) != 2 ||
		d.Photos[0].Annotations[1].Text != "baris1\nbaris2" || d.Photos[1].Annotations[0].Text != "foto B kotak 1" {
		t.Fatalf("detail: %+v", d)
	}
	if d.Submission.ClientIP == nil {
		t.Fatal("IP klien harus tercatat")
	}

	// replay dengan client_request_id yang sama: 200, submission dan token yang sama, tanpa baris baru
	again := st.submit(t, submitBody(reqID, a, b))
	if again.Status != 200 || again.SubmissionID != res.SubmissionID || again.EditToken != res.EditToken {
		t.Fatalf("replay: %+v", again)
	}
	var n int
	_ = st.pool.QueryRow(context.Background(), "SELECT count(*) FROM submissions").Scan(&n)
	if n != 1 {
		t.Fatalf("submissions = %d, want 1", n)
	}

	// foto yang sama tidak bisa dipakai submission baru
	dup := st.submit(t, submitBody(uuid.New(), a))
	if dup.Status != 409 || dup.Code != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("pemakaian ganda: %+v", dup)
	}
}

func TestE2ESubmitRejectsBadInputs(t *testing.T) {
	st := newStack(t, stackOpts{})
	good := st.uploadedPhoto(t, 0x33)

	t.Run("foto belum di-upload", func(t *testing.T) {
		pending := st.presign(t, "image/webp", 8000, 1920, 1440).ID
		r := st.submit(t, submitBody(uuid.New(), pending))
		if r.Status != 422 || r.Code != "PHOTO_NOT_UPLOADED" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("foto tidak ada", func(t *testing.T) {
		r := st.submit(t, submitBody(uuid.New(), uuid.New()))
		if r.Status != 422 || r.Code != "PHOTO_NOT_FOUND" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("bbox keluar dari foto", func(t *testing.T) {
		body := `{"photos":[{"photo_id":"` + good.String() + `","annotations":[{"text":"ok","x":0.9,"y":0,"w":0.5,"h":0.5}]}],"client_request_id":"` + uuid.NewString() + `"}`
		r := st.submit(t, body)
		if r.Status != 422 || r.Code != "INVALID_BBOX" || !strings.Contains(r.Message, "photos[0].annotations[0]") {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("teks anotasi kosong", func(t *testing.T) {
		body := `{"photos":[{"photo_id":"` + good.String() + `","annotations":[{"text":"  ","x":0,"y":0,"w":0.5,"h":0.5}]}],"client_request_id":"` + uuid.NewString() + `"}`
		if r := st.submit(t, body); r.Status != 422 || r.Code != "INVALID_ANNOTATION_TEXT" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("tanpa foto", func(t *testing.T) {
		r := st.submit(t, `{"photos":[],"client_request_id":"`+uuid.NewString()+`"}`)
		if r.Status != 422 || r.Code != "INVALID_PHOTO_COUNT" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("client_request_id nol", func(t *testing.T) {
		r := st.submit(t, submitBody(uuid.Nil, good))
		if r.Status != 422 || r.Code != "INVALID_CLIENT_REQUEST_ID" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("file dihapus dari disk setelah upload", func(t *testing.T) {
		gone := st.uploadedPhoto(t, 0x44)
		p := st.photo(t, gone)
		if err := removeFile(st, p); err != nil {
			t.Fatal(err)
		}
		r := st.submit(t, submitBody(uuid.New(), gone))
		if r.Status != 422 || r.Code != "PHOTO_FILE_INVALID" {
			t.Fatalf("%+v", r)
		}
		if st.photo(t, gone).Status != domain.PhotoRejected {
			t.Fatal("foto harus rejected")
		}
	})
	// setelah semua kegagalan itu foto yang sehat masih bisa dipakai
	if r := st.submit(t, submitBody(uuid.New(), good)); r.Status != 201 {
		t.Fatalf("foto sehat harus tetap bisa dipakai: %+v", r)
	}
}

func TestE2ESubmitConcurrentDuplicateRequests(t *testing.T) {
	st := newStack(t, stackOpts{})
	a := st.uploadedPhoto(t, 0x55)
	reqID := uuid.New()
	body := submitBody(reqID, a)
	const n = 8
	results := make(chan submitResp, n)
	for i := 0; i < n; i++ {
		go func() { results <- st.submit(t, body) }()
	}
	created, replays := 0, 0
	var id uuid.UUID
	for i := 0; i < n; i++ {
		r := <-results
		switch r.Status {
		case 201:
			created++
		case 200:
			replays++
		default:
			t.Fatalf("status tak terduga: %+v", r)
		}
		if id == uuid.Nil {
			id = r.SubmissionID
		} else if r.SubmissionID != id {
			t.Fatal("semua respons harus merujuk submission yang sama")
		}
	}
	if created != 1 || replays != n-1 {
		t.Fatalf("dibuat=%d replay=%d", created, replays)
	}
}

var _ = bytes.NewReader
var _ = http.StatusOK
