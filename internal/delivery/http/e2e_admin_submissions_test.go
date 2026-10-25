package httpapi_test

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type adminListResp struct {
	Items []struct {
		ID                 string         `json:"id"`
		Version            int            `json:"version"`
		Review             map[string]any `json:"review"`
		RetentionDeletesAt *string        `json:"retention_deletes_at"`
		Annotator          *struct {
			ID, Name, Email, Phone string
			Blocked                bool
		} `json:"annotator"`
		PhotoCount      int              `json:"photo_count"`
		AnnotationCount int              `json:"annotation_count"`
		Labels          []map[string]any `json:"labels"`
		LegacyTextCount int              `json:"legacy_text_count"`
		Cover           *struct {
			PhotoID     string           `json:"photo_id"`
			URL         string           `json:"url"`
			Width       int              `json:"width"`
			Annotations []map[string]any `json:"annotations"`
		} `json:"cover"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (s *stack) adminList(t *testing.T, query string) adminListResp {
	t.Helper()
	code, hdr, raw := s.get(t, "/api/v1/admin/submissions"+query, nil)
	if code != 200 || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("admin/submissions%s: %d %s", query, code, raw)
	}
	var out adminListResp
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestE2EAdminSubmissionsOTPMode(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	alice := s.login(t, "081200000001", "6281200000001")
	bob := s.login(t, "085700000002", "6285700000002")
	a1 := alice.submitted(t, 2)
	b1 := bob.submitted(t, 1)
	admin := s.adminLogin(t)

	res := admin.adminList(t, "")
	if len(res.Items) != 2 || res.Items[0].ID != b1.ID.String() || res.NextCursor != nil {
		t.Fatalf("daftar: %+v", res)
	}
	it := res.Items[1]
	if it.ID != a1.ID.String() || it.Annotator == nil || it.Annotator.Phone != "6281200000001" || it.Annotator.Name != "U" ||
		it.Annotator.Email != "u@example.com" || it.Annotator.Blocked || it.PhotoCount != 2 || it.AnnotationCount != 2 ||
		len(it.Labels) != 2 || it.Labels[0]["name"] != "Label 1" || it.Labels[0]["count"] != 1.0 || it.LegacyTextCount != 0 ||
		it.Review["status"] != "pending" || it.RetentionDeletesAt == nil || it.Version != 1 {
		t.Fatalf("item: %+v", it)
	}
	if it.Cover == nil || it.Cover.PhotoID != a1.Photo[0].String() || it.Cover.Width != 1920 || len(it.Cover.Annotations) != 2 ||
		it.Cover.Annotations[0]["label_id"] != 1.0 || it.Cover.Annotations[0]["x"] != 0.12 {
		t.Fatalf("sampul: %+v", it.Cover)
	}
	if code, _, body := s.get(t, it.Cover.URL, nil); code != 200 || len(body) == 0 {
		t.Fatalf("URL sampul: %d", code)
	}

	// filter
	if r := admin.adminList(t, "?q="+url.QueryEscape("0857")); len(r.Items) != 1 || r.Items[0].ID != b1.ID.String() {
		t.Fatalf("q nomor: %+v", r.Items)
	}
	aliceID := it.Annotator.ID
	if r := admin.adminList(t, "?user_id="+aliceID+"&label_id=2"); len(r.Items) != 1 || r.Items[0].ID != a1.ID.String() {
		t.Fatalf("user_id + label_id: %+v", r.Items)
	}
	// halaman
	p1 := admin.adminList(t, "?limit=1&sort=oldest")
	if len(p1.Items) != 1 || p1.Items[0].ID != a1.ID.String() || p1.NextCursor == nil {
		t.Fatalf("halaman 1: %+v", p1)
	}
	p2 := admin.adminList(t, "?limit=1&sort=oldest&cursor="+*p1.NextCursor)
	if len(p2.Items) != 1 || p2.Items[0].ID != b1.ID.String() || p2.NextCursor != nil {
		t.Fatalf("halaman 2: %+v", p2)
	}
	for _, q := range []string{"limit=0", "limit=51", "limit=x", "from=kemarin", "sort=acak", "label_id=0", "user_id=abc",
		"unlabeled=mungkin", "cursor=rusak", "cursor=" + *p1.NextCursor, // cursor oldest dipakai untuk newest
		"from=2026-10-25T00:00:00Z&to=2026-10-24T00:00:00Z", "q=" + strings.Repeat("a", 101)} {
		if code, _, body := admin.get(t, "/api/v1/admin/submissions?"+q, nil); code != 422 || !strings.Contains(string(body), "INVALID_QUERY") {
			t.Fatalf("%s: %d %s", q, code, body)
		}
	}

	// detail
	code, hdr, raw := admin.get(t, "/api/v1/admin/submissions/"+a1.ID.String(), nil)
	if code != 200 || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("detail: %d %s", code, raw)
	}
	var d map[string]any
	_ = json.Unmarshal(raw, &d)
	photos := d["photos"].([]any)
	p0 := photos[0].(map[string]any)
	if d["annotator"].(map[string]any)["phone"] != "6281200000001" || len(photos) != 2 || len(p0["sha256"].(string)) != 64 ||
		!strings.Contains(p0["url"].(string), "/content?exp=") || d["editable"] != true || d["review"] == nil || d["retention_deletes_at"] == nil {
		t.Fatalf("detail: %s", raw)
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/submissions/"+uuid.NewString(), nil); code != 404 {
		t.Fatalf("detail id tak ada: %d", code)
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/submissions/bukan-uuid", nil); code != 404 {
		t.Fatalf("detail bukan uuid: %d", code)
	}
	// token pengguna dan tanpa token → 401
	for _, c := range []*stack{alice, s.stack} {
		if code, _, _ := c.get(t, "/api/v1/admin/submissions", nil); code != 401 {
			t.Fatalf("bukan admin: %d", code)
		}
		if code, _, _ := c.get(t, "/api/v1/admin/submissions/"+a1.ID.String(), nil); code != 401 {
			t.Fatalf("bukan admin detail: %d", code)
		}
	}
}

func TestE2EAdminSubmissionsModeNoneAnonymous(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	sub := st.submitted(t, 1)
	admin := st.adminLogin(t)
	res := admin.adminList(t, "")
	if len(res.Items) != 1 || res.Items[0].ID != sub.ID.String() || res.Items[0].Annotator != nil {
		t.Fatalf("mode none: %+v", res.Items)
	}
	code, _, raw := admin.get(t, "/api/v1/admin/submissions/"+sub.ID.String(), nil)
	if code != 200 || !strings.Contains(string(raw), `"annotator":null`) {
		t.Fatalf("detail mode none: %d %s", code, raw)
	}
}
