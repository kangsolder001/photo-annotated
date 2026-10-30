package httpapi_test

import "testing"

// spec-pendataan §5.1: GET /survey/form publik, bentuk JSON snake_case.
func TestE2ESurveyForm(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0) // mode otp: tetap publik tanpa sesi
	resp, m := s.getJSON(t, "/api/v1/survey/form")
	if resp.StatusCode != 200 || m["version"] != 1.0 || m["consent_text"] == "" {
		t.Fatalf("form: %d %v", resp.StatusCode, m)
	}
	secs := m["sections"].([]any)
	a := secs[0].(map[string]any)
	if len(secs) != 9 || a["id"] != "A" || a["title"] != "A. Identitas Pendataan" || a["help"] == nil {
		t.Fatalf("bagian A: %v", a)
	}
	rw := a["questions"].([]any)[1].(map[string]any)
	if rw["id"] != "a_rw" || rw["type"] != "text" || rw["required"] != true || rw["max"] != 10.0 || rw["options"] != nil || rw["help"] != nil {
		t.Fatalf("a_rw: %v", rw)
	}
	r3 := secs[8].(map[string]any)
	for _, q := range r3["questions"].([]any) {
		q := q.(map[string]any)
		if q["id"] != "r3_container_types" {
			continue
		}
		opts := q["options"].([]any)
		last, first := opts[len(opts)-1].(map[string]any), opts[0].(map[string]any)
		if q["type"] != "multi" || last["other"] != true || last["id"] != "other" || first["other"] != nil {
			t.Fatalf("multi: %v", q)
		}
		return
	}
	t.Fatal("r3_container_types tidak ada")
}
