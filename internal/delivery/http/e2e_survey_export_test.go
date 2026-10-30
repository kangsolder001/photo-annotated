package httpapi_test

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func (s *stack) exportCSV(t *testing.T, query string) (*http.Response, [][]string, string) {
	t.Helper()
	resp, err := s.client.Get(s.srv.URL + "/api/v1/admin/export.csv" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return resp, nil, string(raw)
	}
	recs, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("CSV tidak valid: %v\n%s", err, raw)
	}
	return resp, recs, string(raw)
}

// spec-pendataan §5.7: satu baris per pendataan, teks opsi, BOM, tanpa nomor HP/email, anti formula.
func TestE2ESurveyExportCSV(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	alice := s.login(t, "081200000001", "6281200000001")
	no := alice.submit(t, surveyBody(uuid.New(), 1, setAnswer(t, surveyNoJSON, "a_household_code", `"=1+1"`)))
	yesAns := setAnswer(t, setAnswer(t, setAnswer(t, surveyYesJSON, "r3_container_types", `["bucket","other"]`),
		"r3_container_types_other", `"Galon, bekas"`), "r2_occupants", "4")
	yes := alice.submit(t, surveyBody(uuid.New(), 1, yesAns, alice.uploadedPhoto(t, 0x51)))
	// lebih dari satu halaman (PageSize 2 di stack uji)
	third := alice.submit(t, surveyBody(uuid.New(), 1, surveyNoJSON))
	for _, r := range []submitResp{no, yes, third} {
		if r.Status != 201 {
			t.Fatalf("submit: %d %s", r.Status, r.Raw)
		}
	}
	if resp, _, _ := s.exportCSV(t, ""); resp.StatusCode != 401 {
		t.Fatalf("tanpa token admin: %d", resp.StatusCode)
	}
	admin := s.adminLogin(t)
	resp, recs, raw := admin.exportCSV(t, "")
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), `attachment; filename="pendataan-`) ||
		resp.Trailer.Get("X-Export-Complete") != "true" || resp.Trailer.Get("X-Export-Rows") != "3" {
		t.Fatalf("header: %v %v", resp.Header, resp.Trailer)
	}
	if !strings.HasPrefix(raw, "\xEF\xBB\xBF") {
		t.Fatal("tanpa BOM")
	}
	head := recs[0]
	head[0] = strings.TrimPrefix(head[0], "\xEF\xBB\xBF")
	if strings.Join(head[:9], ",") != "submission_id,created_at,review_status,mahasiswa,nim,kelompok,photo_count,annotation_count,a_date" {
		t.Fatalf("header: %v", head)
	}
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	if col["r3_container_types_other"] != col["r3_container_types"]+1 || col["b1"] < col["a_consent"] || col["r3_lighting"] != len(head)-1 {
		t.Fatalf("urutan kolom: %v", head)
	}
	for _, banned := range []string{"phone", "email", "6281200000001", "u@example.com"} {
		if strings.Contains(raw, banned) {
			t.Fatalf("CSV memuat %s", banned)
		}
	}
	if len(recs) != 4 {
		t.Fatalf("baris = %d, ingin 1 header + 3", len(recs))
	}
	r1, r2 := recs[1], recs[2]
	if r1[0] != no.SubmissionID.String() || r1[col["a_household_code"]] != "'=1+1" || r1[col["a_consent"]] != "Tidak bersedia" ||
		r1[col["b1"]] != "" || r1[col["photo_count"]] != "0" || r1[col["mahasiswa"]] != "U" || r1[col["nim"]] != "N1200000001" ||
		r1[col["kelompok"]] != "Kelompok Uji" || r1[col["review_status"]] != "pending" {
		t.Fatalf("baris menolak: %v", r1)
	}
	if r2[col["a_consent"]] != "Bersedia" || r2[col["b1"]] != "Ya" || r2[col["c1"]] != "Tidak teramati" || r2[col["r3_sorting"]] != "Sebagian" ||
		r2[col["r3_container_types"]] != "Ember; Lainnya" || r2[col["r3_container_types_other"]] != "Galon, bekas" ||
		r2[col["r2_occupants"]] != "4" || r2[col["photo_count"]] != "1" || r2[col["annotation_count"]] != "1" {
		t.Fatalf("baris bersedia: %v", r2)
	}

	// filter consent
	if _, recs, _ := admin.exportCSV(t, "?consent=yes"); len(recs) != 2 || recs[1][0] != yes.SubmissionID.String() {
		t.Fatalf("consent=yes: %v", recs)
	}
	if _, recs, _ := admin.exportCSV(t, "?review_status=accepted"); len(recs) != 1 {
		t.Fatalf("review accepted: hanya header, dapat %d baris", len(recs))
	}
	if resp, _, raw := admin.exportCSV(t, "?consent=x"); resp.StatusCode != 422 || !strings.Contains(raw, "INVALID_QUERY") {
		t.Fatalf("consent salah: %d %s", resp.StatusCode, raw)
	}
	if resp, _, _ := admin.exportCSV(t, "?from=2020-01-01"); resp.StatusCode != 422 {
		t.Fatalf("from bukan RFC 3339: %d", resp.StatusCode)
	}
}
