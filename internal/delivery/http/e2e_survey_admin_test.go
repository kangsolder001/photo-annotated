package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type surveyListItem struct {
	ID             string  `json:"id"`
	ADate          *string `json:"a_date"`
	ARW            *string `json:"a_rw"`
	ART            *string `json:"a_rt"`
	AHouseholdCode *string `json:"a_household_code"`
	Consent        *bool   `json:"consent"`
	PhotoCount     int     `json:"photo_count"`
	Annotator      struct {
		Name  string  `json:"name"`
		NIM   *string `json:"nim"`
		Group *struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"group"`
	} `json:"annotator"`
}

type surveyList struct {
	Items      []surveyListItem `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

func (s *stack) surveyList(t *testing.T, query string) surveyList {
	t.Helper()
	code, _, raw := s.get(t, "/api/v1/admin/submissions"+query, nil)
	if code != 200 {
		t.Fatalf("admin/submissions%s: %d %s", query, code, raw)
	}
	var out surveyList
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func listIDs(l surveyList) string {
	var ids []string
	for _, it := range l.Items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}

// spec-pendataan §5.3, §5.6: jawaban di GET /submissions/{id} dan detail admin; daftar admin memuat
// ringkasan pendataan (termasuk yang menolak tanpa foto) dengan filter consent, rw, group_id.
func TestE2ESurveyReadAndAdminList(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	alice := s.login(t, "081200000001", "6281200000001")
	bob := s.login(t, "085700000002", "6285700000002")
	admin := s.adminLogin(t)
	_, k2 := admin.send(t, "POST", "/api/v1/admin/groups", `{"name":"Kelompok 2"}`)
	_, users := admin.getJSON(t, "/api/v1/admin/users?q=0857")
	bobID := users["items"].([]any)[0].(map[string]any)["id"].(string)
	admin.send(t, "PUT", "/api/v1/admin/users/"+bobID+"/group", `{"group_id":`+idOf(k2)+`}`)

	aNo := alice.submit(t, surveyBody(uuid.New(), 1, surveyNoJSON))
	aYes := alice.submit(t, surveyBody(uuid.New(), 1, setAnswer(t, surveyYesJSON, "a_rw", `"07"`), alice.uploadedPhoto(t, 0x41)))
	bYes := bob.submit(t, surveyBody(uuid.New(), 1, surveyYesJSON, bob.uploadedPhoto(t, 0x42)))
	for _, r := range []submitResp{aNo, aYes, bYes} {
		if r.Status != 201 {
			t.Fatalf("submit: %d %s", r.Status, r.Raw)
		}
	}

	// pemilik membaca jawaban
	code, _, raw := alice.get(t, "/api/v1/submissions/"+aNo.SubmissionID.String(), nil)
	var v map[string]any
	_ = json.Unmarshal(raw, &v)
	if code != 200 || v["form_version"] != 1.0 || v["consent"] != false || v["answers"].(map[string]any)["a_household_code"] != "RT03-RW05-12" ||
		len(v["photos"].([]any)) != 0 {
		t.Fatalf("GET submission: %d %s", code, raw)
	}

	// daftar admin: semua, terbaru dulu; menolak tanpa foto ikut tampil
	all := admin.surveyList(t, "")
	if listIDs(all) != strings.Join([]string{bYes.SubmissionID.String(), aYes.SubmissionID.String(), aNo.SubmissionID.String()}, ",") {
		t.Fatalf("daftar: %s", listIDs(all))
	}
	no := all.Items[2]
	if *no.ADate != "2020-01-02" || *no.ARW != "05" || *no.ART != "03" || *no.AHouseholdCode != "RT03-RW05-12" || *no.Consent ||
		no.PhotoCount != 0 || no.Annotator.NIM == nil || *no.Annotator.NIM != "N1200000001" || no.Annotator.Group.Name != "Kelompok Uji" {
		t.Fatalf("item menolak: %+v", no)
	}
	if b := all.Items[0]; !*b.Consent || b.Annotator.Group.Name != "Kelompok 2" {
		t.Fatalf("item bob: %+v", b)
	}

	for _, tc := range []struct{ q, want string }{
		{"?consent=no", aNo.SubmissionID.String()},
		{"?consent=yes", bYes.SubmissionID.String() + "," + aYes.SubmissionID.String()},
		{"?rw=05", bYes.SubmissionID.String() + "," + aNo.SubmissionID.String()},
		{"?rw=07&consent=yes", aYes.SubmissionID.String()},
		{"?group_id=" + idOf(k2), bYes.SubmissionID.String()},
		{"?group_id=1&consent=yes", aYes.SubmissionID.String()},
		{"?rw=99", ""},
		{"?review_status=pending&sort=oldest&rw=05", aNo.SubmissionID.String() + "," + bYes.SubmissionID.String()},
	} {
		if got := listIDs(admin.surveyList(t, tc.q)); got != tc.want {
			t.Fatalf("%s: %s, ingin %s", tc.q, got, tc.want)
		}
	}
	// cursor stabil dengan filter
	page1 := admin.surveyList(t, "?rw=05&limit=1")
	if len(page1.Items) != 1 || page1.NextCursor == nil {
		t.Fatalf("halaman 1: %+v", page1)
	}
	page2 := admin.surveyList(t, "?rw=05&limit=1&cursor="+*page1.NextCursor)
	if listIDs(page1)+","+listIDs(page2) != bYes.SubmissionID.String()+","+aNo.SubmissionID.String() || page2.NextCursor != nil {
		t.Fatalf("halaman 2: %+v", page2)
	}
	for _, q := range []string{"?consent=maybe", "?group_id=abc", "?rw=" + strings.Repeat("1", 11)} {
		if code, _, raw := admin.get(t, "/api/v1/admin/submissions"+q, nil); code != 422 || !strings.Contains(string(raw), "INVALID_QUERY") {
			t.Fatalf("%s: %d %s", q, code, raw)
		}
	}

	// detail admin: answers + form_version di akar
	code, _, raw = admin.get(t, "/api/v1/admin/submissions/"+aYes.SubmissionID.String(), nil)
	var d map[string]any
	_ = json.Unmarshal(raw, &d)
	if code != 200 || d["form_version"] != 1.0 || d["consent"] != true || d["answers"].(map[string]any)["b7"] != "yes" ||
		d["annotator"].(map[string]any)["nim"] != "N1200000001" {
		t.Fatalf("detail: %d %s", code, raw)
	}
}
