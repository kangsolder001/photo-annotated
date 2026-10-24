package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestE2EAdminReview(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	admin := st.adminLogin(t)
	sub := st.submitted(t, 1)
	other := st.submitted(t, 1)
	tok := map[string]string{"X-Edit-Token": sub.Token}
	path := "/api/v1/admin/submissions/" + sub.ID.String() + "/review"

	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"status":"rejected","version":1}`, 422, "INVALID_REVIEW_REASON"},
		{`{"status":"rejected","reason":"","version":1}`, 422, "INVALID_REVIEW_REASON"},
		{`{"status":"pending","reason":"x","version":1}`, 422, "INVALID_REVIEW_REASON"},
		{`{"status":"ok","version":1}`, 422, "INVALID_REVIEW_STATUS"},
		{`{"status":"accepted","version":5}`, 409, "VERSION_CONFLICT"},
		{`{"status":"accepted"}`, 422, "INVALID_VERSION"},
		{`{"status":"accepted","version":1,"extra":true}`, 400, "INVALID_JSON"},
	} {
		if resp, m := admin.send(t, "PUT", path, tc.body); resp.StatusCode != tc.status || errField(m) != tc.code {
			t.Fatalf("%s: %d %v", tc.body, resp.StatusCode, m)
		}
	}
	resp, m := admin.send(t, "PUT", path, `{"status":"rejected","reason":"Kotak tidak menutupi seluruh kerusakan","version":1}`)
	rv, _ := m["review"].(map[string]any)
	if resp.StatusCode != 200 || rv["status"] != "rejected" || rv["reason"] != "Kotak tidak menutupi seluruh kerusakan" ||
		rv["reviewed_at"] == nil || m["retention_deletes_at"] == nil || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("tolak: %d %v", resp.StatusCode, m)
	}
	// pemilik melihat status + alasan (tanpa reviewed_at)
	_, _, raw := st.get(t, "/api/v1/submissions/"+sub.ID.String(), tok)
	if !strings.Contains(string(raw), `"review":{"status":"rejected","reason":"Kotak tidak menutupi seluruh kerusakan"}`) {
		t.Fatalf("pemilik: %s", raw)
	}
	if r := admin.adminList(t, "?review_status=rejected"); len(r.Items) != 1 || r.Items[0].ID != sub.ID.String() ||
		r.Items[0].Review["reason"] != "Kotak tidak menutupi seluruh kerusakan" {
		t.Fatalf("filter rejected: %+v", r.Items)
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/submissions?review_status=approved", nil); code != 422 {
		t.Fatalf("review_status salah: %d", code)
	}

	// review massal: terima keduanya
	resp, m = admin.send(t, "POST", "/api/v1/admin/submissions/review",
		`{"items":[{"id":"`+sub.ID.String()+`","version":1},{"id":"`+other.ID.String()+`","version":2},{"id":"`+uuid.NewString()+`","version":1}],"status":"accepted","reason":null}`)
	var results []map[string]any
	if raw, _ := json.Marshal(m["results"]); json.Unmarshal(raw, &results) != nil || resp.StatusCode != 200 || len(results) != 3 ||
		results[0]["result"] != "ok" || results[1]["result"] != "version_conflict" || results[2]["result"] != "not_found" {
		t.Fatalf("review massal: %d %v", resp.StatusCode, m)
	}
	if r := admin.adminList(t, "?review_status=accepted"); len(r.Items) != 1 || r.Items[0].RetentionDeletesAt != nil {
		t.Fatalf("accepted tidak ikut retensi: %+v", r.Items)
	}
	_, _, body := admin.get(t, "/api/v1/admin/export?review_status=accepted", nil)
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"review_status":"accepted"`) || !strings.Contains(lines[0], sub.ID.String()) {
		t.Fatalf("export accepted: %s", body)
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/export?review_status=semua", nil); code != 422 {
		t.Fatalf("export review_status salah: %d", code)
	}

	// edit pemilik mengembalikan ke pending
	if e := st.edit(t, sub.ID, sub.Token, editBody(1, sub.Photo...)); e.Status != 200 {
		t.Fatalf("edit: %+v", e)
	}
	_, _, raw = st.get(t, "/api/v1/submissions/"+sub.ID.String(), tok)
	if !strings.Contains(string(raw), `"review":{"status":"pending","reason":null}`) {
		t.Fatalf("setelah edit: %s", raw)
	}
	// non-admin
	if resp, _ := st.send(t, "PUT", path, `{"status":"accepted","version":2}`); resp.StatusCode != 401 {
		t.Fatalf("tanpa token admin: %d", resp.StatusCode)
	}
	if resp, _ := admin.send(t, "PUT", "/api/v1/admin/submissions/"+uuid.NewString()+"/review", `{"status":"accepted","version":1}`); resp.StatusCode != 404 {
		t.Fatalf("id tak ada: %d", resp.StatusCode)
	}
}
