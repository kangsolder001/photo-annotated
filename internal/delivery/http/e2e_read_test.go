package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/testutil/imgfix"
)

func (s *stack) get(t testing.TB, url string, hdr map[string]string) (int, http.Header, []byte) {
	t.Helper()
	if strings.HasPrefix(url, "/") {
		url = s.srv.URL + url
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

type e2eSubmission struct {
	ID    uuid.UUID
	Token string
	Photo []uuid.UUID
	Body  [][]byte
}

// submitted membuat submission sungguhan lewat HTTP: n foto (isi berbeda), 2 anotasi pada foto pertama.
func (s *stack) submitted(t testing.TB, n int) e2eSubmission {
	t.Helper()
	var out e2eSubmission
	var ps []string
	for i := 0; i < n; i++ {
		fill := byte(0x10 + i)
		id := s.uploadedPhoto(t, fill)
		out.Photo = append(out.Photo, id)
		out.Body = append(out.Body, imgfix.WebP(1920, 1440, 8000, fill))
		ann := ""
		if i == 0 {
			ann = `{"text":"kotak pertama","x":0.12,"y":0.30,"w":0.25,"h":0.18},{"text":"kotak kedua","x":0.55,"y":0.10,"w":0.30,"h":0.12}`
		}
		ps = append(ps, `{"photo_id":"`+id.String()+`","annotations":[`+ann+`]}`)
	}
	res := s.submit(t, `{"photos":[`+strings.Join(ps, ",")+`],"client_request_id":"`+uuid.NewString()+`"}`)
	if res.Status != 201 {
		t.Fatalf("submit: %d %s", res.Status, res.Raw)
	}
	out.ID, out.Token = res.SubmissionID, res.EditToken
	return out
}

func TestE2EReadSubmissionAndViewPhotos(t *testing.T) {
	st := newStack(t, stackOpts{})
	sub := st.submitted(t, 2)
	tok := map[string]string{"X-Edit-Token": sub.Token}

	// ---- GET /submissions/{id}
	code, _, raw := st.get(t, "/api/v1/submissions/"+sub.ID.String(), tok)
	if code != 200 {
		t.Fatalf("status %d: %s", code, raw)
	}
	var v struct {
		ID            uuid.UUID `json:"id"`
		Version       int       `json:"version"`
		Editable      bool      `json:"editable"`
		EditableUntil time.Time `json:"editable_until"`
		CreatedAt     time.Time `json:"created_at"`
		Photos        []struct {
			PhotoID     uuid.UUID `json:"photo_id"`
			Position    int       `json:"position"`
			Width       int       `json:"width"`
			Annotations []struct {
				Text string  `json:"text"`
				X    float64 `json:"x"`
			} `json:"annotations"`
		} `json:"photos"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.ID != sub.ID || v.Version != 1 || !v.Editable || !v.EditableUntil.Equal(v.CreatedAt.Add(24*time.Hour)) {
		t.Fatalf("submission: %s", raw)
	}
	if len(v.Photos) != 2 || v.Photos[0].PhotoID != sub.Photo[0] || v.Photos[1].PhotoID != sub.Photo[1] || v.Photos[0].Width != 1920 {
		t.Fatalf("foto: %s", raw)
	}
	if len(v.Photos[0].Annotations) != 2 || v.Photos[0].Annotations[0].Text != "kotak pertama" || v.Photos[0].Annotations[0].X != 0.12 ||
		len(v.Photos[1].Annotations) != 0 {
		t.Fatalf("anotasi: %s", raw)
	}
	for _, internal := range []string{"storage_key", "sha256", "owner", "client_ip", "client_request_id"} {
		if strings.Contains(string(raw), internal) {
			t.Fatalf("respons membocorkan %q: %s", internal, raw)
		}
	}

	// ---- GET /photos/{id}/url → fetch konten TANPA token (seperti <img src>)
	code, _, raw = st.get(t, "/api/v1/photos/"+sub.Photo[1].String()+"/url", tok)
	if code != 200 {
		t.Fatalf("photo url: %d %s", code, raw)
	}
	var pu struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &pu); err != nil {
		t.Fatal(err)
	}
	if d := time.Until(pu.ExpiresAt); d < 4*time.Minute || d > 5*time.Minute+time.Second {
		t.Fatalf("masa berlaku URL = %v, want ±5 menit", d)
	}
	code, hdr, body := st.get(t, pu.URL, nil)
	if code != 200 || !bytes.Equal(body, sub.Body[1]) {
		t.Fatalf("konten: %d (%d byte, want %d)", code, len(body), len(sub.Body[1]))
	}
	sum := sha256.Sum256(sub.Body[1])
	if hdr.Get("Content-Type") != "image/webp" || hdr.Get("X-Content-Type-Options") != "nosniff" ||
		hdr.Get("Content-Disposition") != "inline" || hdr.Get("ETag") != `"`+hex.EncodeToString(sum[:])+`"` ||
		hdr.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("header: %v", hdr)
	}

	// Range
	code, hdr, part := st.get(t, pu.URL, map[string]string{"Range": "bytes=100-199"})
	if code != 206 || !bytes.Equal(part, sub.Body[1][100:200]) || !strings.HasPrefix(hdr.Get("Content-Range"), "bytes 100-199/") {
		t.Fatalf("range: %d %v", code, hdr)
	}
	// If-None-Match → 304
	code, _, body = st.get(t, pu.URL, map[string]string{"If-None-Match": `"` + hex.EncodeToString(sum[:]) + `"`})
	if code != 304 || len(body) != 0 {
		t.Fatalf("conditional: %d %d byte", code, len(body))
	}
}

func TestE2EReadAccessIsUniform404(t *testing.T) {
	st := newStack(t, stackOpts{})
	mine, other := st.submitted(t, 1), st.submitted(t, 1)

	var bodies []string
	for name, c := range map[string]struct {
		path string
		hdr  map[string]string
	}{
		"submission tanpa token":      {"/api/v1/submissions/" + mine.ID.String(), nil},
		"submission token salah":      {"/api/v1/submissions/" + mine.ID.String(), map[string]string{"X-Edit-Token": "salah"}},
		"submission token orang lain": {"/api/v1/submissions/" + mine.ID.String(), map[string]string{"X-Edit-Token": other.Token}},
		"submission tidak ada":        {"/api/v1/submissions/" + uuid.NewString(), map[string]string{"X-Edit-Token": mine.Token}},
	} {
		code, _, raw := st.get(t, c.path, c.hdr)
		if code != 404 {
			t.Errorf("%s: %d %s", name, code, raw)
		}
		bodies = append(bodies, string(raw))
	}
	for name, c := range map[string]struct {
		pid uuid.UUID
		hdr map[string]string
	}{
		"url tanpa token":      {mine.Photo[0], nil},
		"url token orang lain": {mine.Photo[0], map[string]string{"X-Edit-Token": other.Token}},
		"url foto orang lain":  {other.Photo[0], map[string]string{"X-Edit-Token": mine.Token}},
		"url foto tidak ada":   {uuid.New(), map[string]string{"X-Edit-Token": mine.Token}},
	} {
		code, _, raw := st.get(t, "/api/v1/photos/"+c.pid.String()+"/url", c.hdr)
		if code != 404 {
			t.Errorf("%s: %d %s", name, code, raw)
		}
		bodies = append(bodies, string(raw))
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("semua 404 harus berisi badan yang sama (bukan oracle): %q vs %q", bodies[0], b)
		}
	}

	// foto yang belum dipakai submission (pending) tidak bisa dilihat lewat jalur ini
	pending := st.uploadedPhoto(t, 0x77)
	if code, _, _ := st.get(t, "/api/v1/photos/"+pending.String()+"/url", map[string]string{"X-Edit-Token": mine.Token}); code != 404 {
		t.Fatalf("foto pending: %d", code)
	}
}

func TestE2EPhotoContentSignatureAndExpiry(t *testing.T) {
	st := newStack(t, stackOpts{viewTTL: time.Second})
	sub := st.submitted(t, 1)
	code, _, raw := st.get(t, "/api/v1/photos/"+sub.Photo[0].String()+"/url", map[string]string{"X-Edit-Token": sub.Token})
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var pu struct{ URL string }
	_ = json.Unmarshal(raw, &pu)

	// tanda tangan diubah → 403
	tampered := tamperSig(pu.URL)
	if code, _, raw := st.get(t, tampered, nil); code != 403 || !strings.Contains(string(raw), "INVALID_SIGNATURE") {
		t.Fatalf("tampered: %d %s", code, raw)
	}
	// id tak dikenal dengan tanda tangan sah foto lain → 403 yang sama
	other := strings.Replace(pu.URL, sub.Photo[0].String(), uuid.NewString(), 1)
	if code, _, raw := st.get(t, other, nil); code != 403 || !strings.Contains(string(raw), "INVALID_SIGNATURE") {
		t.Fatalf("id tak dikenal: %d %s", code, raw)
	}
	// tanpa parameter sama sekali → 403
	if code, _, _ := st.get(t, "/api/v1/photos/"+sub.Photo[0].String()+"/content", nil); code != 403 {
		t.Fatalf("tanpa sig: %d", code)
	}
	// sah sekarang, kedaluwarsa setelah TTL
	if code, _, _ := st.get(t, pu.URL, nil); code != 200 {
		t.Fatalf("sebelum kedaluwarsa: %d", code)
	}
	time.Sleep(2200 * time.Millisecond)
	if code, _, raw := st.get(t, pu.URL, nil); code != 410 || !strings.Contains(string(raw), "URL_EXPIRED") {
		t.Fatalf("setelah kedaluwarsa: %d %s", code, raw)
	}
}

func TestE2EViewURLCannotBeUsedToUpload(t *testing.T) {
	st := newStack(t, stackOpts{})
	sub := st.submitted(t, 1)
	_, _, raw := st.get(t, "/api/v1/photos/"+sub.Photo[0].String()+"/url", map[string]string{"X-Edit-Token": sub.Token})
	var pu struct{ URL string }
	_ = json.Unmarshal(raw, &pu)
	putURL := strings.Replace(pu.URL, "/content?", "?", 1)
	putURL = strings.Replace(putURL, "/photos/", "/uploads/", 1)
	r := st.putBytes(t, putURL, "image/webp", sub.Body[0])
	if r.Status != 403 || r.Code != "INVALID_SIGNATURE" {
		t.Fatalf("URL lihat tidak boleh berlaku untuk upload: %+v", r)
	}
}

func TestE2EReadHasCORSForAllowedOrigin(t *testing.T) {
	st := newStack(t, stackOpts{})
	sub := st.submitted(t, 1)
	code, hdr, _ := st.get(t, "/api/v1/submissions/"+sub.ID.String(), map[string]string{"X-Edit-Token": sub.Token, "Origin": "https://app.example.com"})
	if code != 200 || hdr.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("%d %v", code, hdr)
	}
}
