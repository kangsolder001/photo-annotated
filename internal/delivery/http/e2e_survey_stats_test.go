package httpapi_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

type surveyStatsResp struct {
	Totals struct{ Households, Consented, Declined int } `json:"totals"`
	ByRW   []struct {
		RW                              string `json:"rw"`
		Households, Consented, Declined int
	} `json:"by_rw"`
	ByStudent []struct {
		UserID     string  `json:"user_id"`
		Name       string  `json:"name"`
		NIM        *string `json:"nim"`
		Group      *string `json:"group"`
		Households int     `json:"households"`
		Declined   int     `json:"declined"`
	} `json:"by_student"`
	B []struct {
		ID, Label string
		Yes, No   int
		YesShare  float64 `json:"yes_share"`
	} `json:"b"`
	C []struct {
		ID, Label   string
		Yes, No, NA int
	} `json:"c"`
	R3Sorting map[string]int `json:"r3_sorting"`
}

func (s *stack) surveyStats(t *testing.T, query string) (int, surveyStatsResp) {
	t.Helper()
	code, _, raw := s.get(t, "/api/v1/admin/stats/survey"+query, nil)
	var out surveyStatsResp
	if code == 200 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return code, out
}

// spec-pendataan §5.8: 3 pendataan (2 bersedia, 1 menolak, 2 RW) → angka dihitung manual.
func TestE2ESurveyStats(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	alice := s.login(t, "081200000001", "6281200000001")
	bob := s.login(t, "085700000002", "6285700000002")
	admin := s.adminLogin(t)
	_, users := admin.getJSON(t, "/api/v1/admin/users?q=0857")
	bobID := users["items"].([]any)[0].(map[string]any)["id"].(string)
	s.pool.Exec(t.Context(), `UPDATE users SET group_id = NULL WHERE id = $1`, bobID) // bob tanpa kelompok

	aliceYes := setAnswer(t, setAnswer(t, setAnswer(t, surveyYesJSON, "b1", `"no"`), "c1", `"yes"`), "r3_sorting", `"sorted"`)
	bobYes := setAnswer(t, setAnswer(t, surveyYesJSON, "a_rw", `"07"`), "a_date", `"2021-05-05"`)
	for _, r := range []submitResp{
		alice.submit(t, surveyBody(uuid.New(), 1, surveyNoJSON)),
		alice.submit(t, surveyBody(uuid.New(), 1, aliceYes, alice.uploadedPhoto(t, 0x61))),
		bob.submit(t, surveyBody(uuid.New(), 1, bobYes, bob.uploadedPhoto(t, 0x62))),
	} {
		if r.Status != 201 {
			t.Fatalf("submit: %d %s", r.Status, r.Raw)
		}
	}
	// submission terhapus tidak dihitung
	gone := alice.submit(t, surveyBody(uuid.New(), 1, surveyNoJSON))
	if resp, _ := admin.send(t, "DELETE", "/api/v1/admin/submissions/"+gone.SubmissionID.String(), ""); resp.StatusCode >= 300 {
		t.Fatalf("hapus: %d", resp.StatusCode)
	}

	code, st := admin.surveyStats(t, "")
	if code != 200 || st.Totals.Households != 3 || st.Totals.Consented != 2 || st.Totals.Declined != 1 {
		t.Fatalf("totals: %d %+v", code, st.Totals)
	}
	if len(st.ByRW) != 2 || st.ByRW[0].RW != "05" || st.ByRW[0].Households != 2 || st.ByRW[0].Consented != 1 || st.ByRW[0].Declined != 1 ||
		st.ByRW[1].RW != "07" || st.ByRW[1].Households != 1 || st.ByRW[1].Declined != 0 {
		t.Fatalf("by_rw: %+v", st.ByRW)
	}
	a, b := st.ByStudent[0], st.ByStudent[1]
	if len(st.ByStudent) != 2 || a.Name != "U" || *a.NIM != "N1200000001" || *a.Group != "Kelompok Uji" || a.Households != 2 || a.Declined != 1 ||
		b.UserID != bobID || b.Group != nil || b.Households != 1 || b.Declined != 0 {
		t.Fatalf("by_student: %+v", st.ByStudent)
	}
	if len(st.B) != 30 || st.B[0].ID != "b1" || st.B[0].Label != "Apakah keluarga mengetahui pentingnya memilah sampah dari rumah?" ||
		st.B[0].Yes != 1 || st.B[0].No != 1 || st.B[0].YesShare != 0.5 || st.B[1].Yes != 2 || st.B[1].YesShare != 1 {
		t.Fatalf("b: %+v", st.B[:2])
	}
	if len(st.C) != 10 || st.C[0].ID != "c1" || st.C[0].Yes != 1 || st.C[0].NA != 1 || st.C[0].No != 0 || st.C[1].NA != 2 {
		t.Fatalf("c: %+v", st.C[:2])
	}
	if len(st.R3Sorting) != 4 || st.R3Sorting["sorted"] != 1 || st.R3Sorting["partial"] != 1 || st.R3Sorting["mixed"] != 0 {
		t.Fatalf("r3_sorting: %v", st.R3Sorting)
	}

	// filter RW dan tanggal (inklusif, atas a_date)
	if _, st := admin.surveyStats(t, "?rw=07"); st.Totals.Households != 1 || st.Totals.Consented != 1 || st.B[0].Yes != 1 || len(st.ByStudent) != 1 {
		t.Fatalf("rw=07: %+v", st)
	}
	if _, st := admin.surveyStats(t, "?from=2021-01-01"); st.Totals.Households != 1 || st.ByRW[0].RW != "07" {
		t.Fatalf("from: %+v", st.Totals)
	}
	if _, st := admin.surveyStats(t, "?from=2020-01-02&to=2020-01-02"); st.Totals.Households != 2 || st.Totals.Declined != 1 || st.R3Sorting["sorted"] != 1 {
		t.Fatalf("to inklusif: %+v", st.Totals)
	}
	if _, st := admin.surveyStats(t, "?rw=99"); st.Totals.Households != 0 || len(st.ByRW) != 0 || st.B[0].YesShare != 0 || st.R3Sorting["sorted"] != 0 {
		t.Fatalf("kosong: %+v", st)
	}
	for _, q := range []string{"?from=2021-13-01", "?to=2021-01-01T00:00:00Z", "?from=2021-02-01&to=2021-01-01"} {
		if code, _ := admin.surveyStats(t, q); code != 422 {
			t.Fatalf("%s: %d", q, code)
		}
	}
	if code, _ := s.surveyStats(t, ""); code != 401 {
		t.Fatalf("tanpa token admin: %d", code)
	}
}
