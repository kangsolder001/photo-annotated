package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type editResp struct {
	Status    int
	Version   int
	UpdatedAt time.Time
	Code      string
	Message   string
	Raw       string
}

func (s *stack) edit(t testing.TB, id uuid.UUID, token, body string) editResp {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, s.srv.URL+"/api/v1/submissions/"+id.String(), strings.NewReader(withSurvey(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Edit-Token", token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := editResp{Status: resp.StatusCode, Raw: string(raw)}
	var j struct {
		Version   int       `json:"version"`
		UpdatedAt time.Time `json:"updated_at"`
		Error     struct{ Code, Message string }
	}
	if json.Unmarshal(raw, &j) == nil {
		out.Version, out.UpdatedAt, out.Code, out.Message = j.Version, j.UpdatedAt, j.Error.Code, j.Error.Message
	}
	return out
}

func editBody(version int, photos ...uuid.UUID) string {
	var ps []string
	for i, p := range photos {
		ps = append(ps, `{"photo_id":"`+p.String()+`","annotations":[{"label_id":`+itoa(3+i%3)+`,"x":0.2,"y":0.2,"w":0.3,"h":0.3}]}`)
	}
	return `{"version":` + itoa(version) + `,"photos":[` + strings.Join(ps, ",") + `]}`
}

func (s *stack) readVersion(t testing.TB, sub e2eSubmission) (version int, texts []string, photoIDs []uuid.UUID) {
	t.Helper()
	code, _, raw := s.get(t, "/api/v1/submissions/"+sub.ID.String(), map[string]string{"X-Edit-Token": sub.Token})
	if code != 200 {
		t.Fatalf("GET: %d %s", code, raw)
	}
	var v struct {
		Version int `json:"version"`
		Photos  []struct {
			PhotoID     uuid.UUID `json:"photo_id"`
			Annotations []struct {
				Label *struct{ Name string } `json:"label"`
			}
		} `json:"photos"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, p := range v.Photos {
		photoIDs = append(photoIDs, p.PhotoID)
		for _, a := range p.Annotations {
			name := ""
			if a.Label != nil {
				name = a.Label.Name
			}
			texts = append(texts, name)
		}
	}
	return v.Version, texts, photoIDs
}

func TestE2EEditFullFlow(t *testing.T) {
	st := newStack(t, stackOpts{})
	sub := st.submitted(t, 2)
	extra := st.uploadedPhoto(t, 0x66)

	// buang foto 2, tambah foto baru, urutan: baru, foto 1
	res := st.edit(t, sub.ID, sub.Token, editBody(1, extra, sub.Photo[0]))
	if res.Status != 200 || res.Version != 2 || time.Since(res.UpdatedAt) > time.Minute {
		t.Fatalf("edit: %+v", res)
	}
	v, texts, ids := st.readVersion(t, sub)
	if v != 2 || len(ids) != 2 || ids[0] != extra || ids[1] != sub.Photo[0] {
		t.Fatalf("hasil: v=%d ids=%v", v, ids)
	}
	if len(texts) != 2 || texts[0] != "Label 3" || texts[1] != "Label 4" {
		t.Fatalf("anotasi: %v", texts)
	}

	// foto yang dibuang tidak bisa lagi dilihat
	if code, _, _ := st.get(t, "/api/v1/photos/"+sub.Photo[1].String()+"/url", map[string]string{"X-Edit-Token": sub.Token}); code != 404 {
		t.Fatalf("foto yang dibuang harus 404 pada /url: %d", code)
	}
	// foto baru kini bisa dilihat
	if code, _, _ := st.get(t, "/api/v1/photos/"+extra.String()+"/url", map[string]string{"X-Edit-Token": sub.Token}); code != 200 {
		t.Fatalf("foto baru harus bisa dilihat: %d", code)
	}

	// versi basi → 409 dan tak ada yang berubah
	stale := st.edit(t, sub.ID, sub.Token, editBody(1, extra, sub.Photo[0]))
	if stale.Status != 409 || stale.Code != "VERSION_CONFLICT" {
		t.Fatalf("versi basi: %+v", stale)
	}
	if v, _, _ := st.readVersion(t, sub); v != 2 {
		t.Fatalf("versi = %d", v)
	}
}

func TestE2EEditAuthorization(t *testing.T) {
	st := newStack(t, stackOpts{})
	mine, other := st.submitted(t, 1), st.submitted(t, 1)

	var bodies []string
	for name, c := range map[string]struct {
		id    uuid.UUID
		token string
	}{
		"tanpa token":      {mine.ID, ""},
		"token salah":      {mine.ID, "salah"},
		"token orang lain": {mine.ID, other.Token},
		"id tidak ada":     {uuid.New(), mine.Token},
	} {
		r := st.edit(t, c.id, c.token, editBody(1, mine.Photo[0]))
		if r.Status != 404 {
			t.Errorf("%s: %d %s", name, r.Status, r.Raw)
		}
		bodies = append(bodies, r.Raw)
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("semua 404 harus berbadan sama: %q vs %q", bodies[0], b)
		}
	}
	if v, texts, _ := st.readVersion(t, mine); v != 1 || len(texts) != 2 {
		t.Fatalf("penolakan tidak boleh mengubah data: v=%d %v", v, texts)
	}
}

func TestE2EEditValidation(t *testing.T) {
	st := newStack(t, stackOpts{})
	sub := st.submitted(t, 1)
	for name, tc := range map[string]struct {
		body string
		code string
	}{
		"tanpa foto":   {`{"version":1,"photos":[]}`, "INVALID_PHOTO_COUNT"},
		"versi hilang": {`{"photos":[{"photo_id":"` + sub.Photo[0].String() + `"}]}`, "INVALID_VERSION"},
		"foto tak ada": {editBody(1, uuid.New()), "PHOTO_NOT_FOUND"},
		"bbox salah":   {`{"version":1,"photos":[{"photo_id":"` + sub.Photo[0].String() + `","annotations":[{"label_id":1,"x":0.9,"y":0,"w":0.5,"h":0.5}]}]}`, "INVALID_BBOX"},
		"foto kembar":  {editBody(1, sub.Photo[0], sub.Photo[0]), "DUPLICATE_PHOTO"},
	} {
		r := st.edit(t, sub.ID, sub.Token, tc.body)
		if r.Status != 422 || r.Code != tc.code {
			t.Errorf("%s: %+v", name, r)
		}
	}
	// foto yang sudah dipakai submission lain
	other := st.submitted(t, 1)
	if r := st.edit(t, sub.ID, sub.Token, editBody(1, sub.Photo[0], other.Photo[0])); r.Status != 409 || r.Code != "PHOTO_NOT_AVAILABLE" {
		t.Fatalf("foto milik submission lain: %+v", r)
	}
}

func TestE2EConcurrentEditsSameVersion(t *testing.T) {
	st := newStack(t, stackOpts{})
	sub := st.submitted(t, 1)
	const n = 8
	results := make(chan editResp, n)
	for i := 0; i < n; i++ {
		go func() { results <- st.edit(t, sub.ID, sub.Token, editBody(1, sub.Photo[0])) }()
	}
	wins, conflicts := 0, 0
	for i := 0; i < n; i++ {
		r := <-results
		switch {
		case r.Status == 200:
			wins++
		case r.Status == 409 && r.Code == "VERSION_CONFLICT":
			conflicts++
		default:
			t.Fatalf("hasil tak terduga: %+v", r)
		}
	}
	if wins != 1 || conflicts != n-1 {
		t.Fatalf("menang=%d konflik=%d", wins, conflicts)
	}
}

var _ = bytes.NewReader
