package httpapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// surveyBody: body submit pendataan dengan jawaban answers (objek JSON) dan foto berkotak.
func surveyBody(reqID uuid.UUID, version int, answers string, photos ...uuid.UUID) string {
	var ps []string
	for _, p := range photos {
		ps = append(ps, `{"photo_id":"`+p.String()+`","annotations":[{"label_id":1,"x":0.1,"y":0.2,"w":0.3,"h":0.3}]}`)
	}
	head := `{"client_request_id":"` + reqID.String() + `",`
	if reqID == uuid.Nil {
		head = `{`
	}
	return head + `"form_version":` + itoa(version) + `,"answers":` + answers + `,"photos":[` + strings.Join(ps, ",") + `]}`
}

// setAnswer mengganti/menambah satu jawaban pada objek JSON (value = JSON mentah; "" = hapus).
func setAnswer(t *testing.T, answers, id, value string) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(answers), &m); err != nil {
		t.Fatal(err)
	}
	if value == "" {
		delete(m, id)
	} else {
		m[id] = json.RawMessage(value)
	}
	b, _ := json.Marshal(m)
	return string(b)
}

type surveyRow struct {
	FormVersion *int
	Answers     map[string]any
	Consent     *bool
	SurveyDate  *time.Time
	RW          *string
}

func (s *stack) surveyRow(t *testing.T, id uuid.UUID) surveyRow {
	t.Helper()
	var r surveyRow
	if err := s.pool.QueryRow(context.Background(), `SELECT form_version, answers, consent, survey_date, rw FROM submissions WHERE id = $1`, id).
		Scan(&r.FormVersion, &r.Answers, &r.Consent, &r.SurveyDate, &r.RW); err != nil {
		t.Fatal(err)
	}
	return r
}

// spec-pendataan §4.3, §5.3: jumlah foto mengikuti persetujuan; jawaban divalidasi dengan lokasi field.
func TestE2ESurveySubmitRules(t *testing.T) {
	st := newStack(t, stackOpts{})
	p := func(fill byte) uuid.UUID { return st.uploadedPhoto(t, fill) }

	for _, tc := range []struct {
		name, body, code, msg string
	}{
		{"menolak + 1 foto", surveyBody(uuid.New(), 1, surveyNoJSON, p(0x01)), "INVALID_PHOTO_COUNT", ""},
		{"bersedia + 0 foto", surveyBody(uuid.New(), 1, surveyYesJSON), "INVALID_PHOTO_COUNT", ""},
		{"bersedia + 4 foto", surveyBody(uuid.New(), 1, surveyYesJSON, p(0x02), p(0x03), p(0x04), p(0x05)), "INVALID_PHOTO_COUNT", ""},
		{"b7 kosong", surveyBody(uuid.New(), 1, setAnswer(t, surveyYesJSON, "b7", ""), p(0x06)), "INVALID_ANSWER", "answers.b7: wajib diisi"},
		{"menolak + jawaban B", surveyBody(uuid.New(), 1, setAnswer(t, surveyNoJSON, "b1", `"yes"`)), "INVALID_ANSWER", "answers.b1"},
		{"id tak dikenal", surveyBody(uuid.New(), 1, setAnswer(t, surveyNoJSON, "zz", `"x"`)), "INVALID_ANSWER", "answers.zz"},
		{"versi 2", surveyBody(uuid.New(), 2, surveyNoJSON), "UNSUPPORTED_FORM_VERSION", ""},
		{"tanpa form_version", `{"client_request_id":"` + uuid.NewString() + `","answers":` + surveyNoJSON + `,"photos":[]}`, "UNSUPPORTED_FORM_VERSION", ""},
	} {
		r := st.submit(t, tc.body)
		if r.Status != 422 || r.Code != tc.code || !strings.Contains(r.Message, tc.msg) {
			t.Fatalf("%s: %d %s", tc.name, r.Status, r.Raw)
		}
	}

	// menolak + 0 foto → 201; replay idempoten → 200 dengan id yang sama
	reqID := uuid.New()
	body := surveyBody(reqID, 1, setAnswer(t, surveyNoJSON, "a_household_code", `"  RT03-RW05-99  "`))
	r := st.submit(t, body)
	if r.Status != 201 {
		t.Fatalf("menolak: %d %s", r.Status, r.Raw)
	}
	if again := st.submit(t, body); again.Status != 200 || again.SubmissionID != r.SubmissionID {
		t.Fatalf("replay: %d %s", again.Status, again.Raw)
	}
	row := st.surveyRow(t, r.SubmissionID)
	if *row.FormVersion != 1 || *row.Consent || row.SurveyDate.Format(time.DateOnly) != "2020-01-02" || *row.RW != "05" ||
		row.Answers["a_household_code"] != "RT03-RW05-99" || len(row.Answers) != 7 {
		t.Fatalf("tersimpan: %+v", row)
	}

	// bersedia + 1 foto berkotak → 201, consent true
	r = st.submit(t, surveyBody(uuid.New(), 1, setAnswer(t, surveyYesJSON, "r2_occupants", "4"), p(0x07)))
	if r.Status != 201 {
		t.Fatalf("bersedia: %d %s", r.Status, r.Raw)
	}
	if row := st.surveyRow(t, r.SubmissionID); !*row.Consent || row.Answers["r2_occupants"] != 4.0 {
		t.Fatalf("bersedia tersimpan: %+v", row)
	}
}

// Edit mengganti jawaban beserta survey_date/rw/consent; bersedia → menolak membuang foto; review kembali
// pending; VERSION_CONFLICT tetap berlaku.
func TestE2ESurveyEdit(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	ph := st.uploadedPhoto(t, 0x31)
	r := st.submit(t, surveyBody(uuid.New(), 1, surveyYesJSON, ph))
	if r.Status != 201 {
		t.Fatalf("submit: %d %s", r.Status, r.Raw)
	}
	tok := r.EditToken
	admin := st.adminLogin(t)
	if resp, m := admin.send(t, "PUT", "/api/v1/admin/submissions/"+r.SubmissionID.String()+"/review", `{"status":"accepted","version":1}`); resp.StatusCode != 200 {
		t.Fatalf("review: %d %v", resp.StatusCode, m)
	}

	ans := setAnswer(t, setAnswer(t, surveyYesJSON, "a_date", `"2021-03-04"`), "a_rw", `"07"`)
	if e := st.edit(t, r.SubmissionID, tok, `{"version":1,"form_version":1,"answers":`+ans+`,"photos":[{"photo_id":"`+ph.String()+`","annotations":[]}]}`); e.Status != 200 || e.Version != 2 {
		t.Fatalf("edit: %d %s", e.Status, e.Raw)
	}
	row := st.surveyRow(t, r.SubmissionID)
	if row.SurveyDate.Format(time.DateOnly) != "2021-03-04" || *row.RW != "07" || !*row.Consent {
		t.Fatalf("salinan tidak ikut berubah: %+v", row)
	}
	var review string
	_ = st.pool.QueryRow(context.Background(), `SELECT review_status FROM submissions WHERE id = $1`, r.SubmissionID).Scan(&review)
	if review != "pending" {
		t.Fatalf("review harus kembali pending: %s", review)
	}

	// versi basi → 409; jawaban salah → 422 sebelum cek versi
	if e := st.edit(t, r.SubmissionID, tok, `{"version":1,"form_version":1,"answers":`+surveyNoJSON+`,"photos":[]}`); e.Status != 409 || e.Code != "VERSION_CONFLICT" {
		t.Fatalf("versi basi: %d %s", e.Status, e.Raw)
	}
	if e := st.edit(t, r.SubmissionID, tok, `{"version":2,"form_version":1,"answers":`+surveyNoJSON+`,"photos":[{"photo_id":"`+ph.String()+`","annotations":[]}]}`); e.Status != 422 || e.Code != "INVALID_PHOTO_COUNT" {
		t.Fatalf("menolak + foto: %d %s", e.Status, e.Raw)
	}
	// bersedia → menolak: foto dibuang, consent false
	if e := st.edit(t, r.SubmissionID, tok, `{"version":2,"form_version":1,"answers":`+surveyNoJSON+`,"photos":[]}`); e.Status != 200 {
		t.Fatalf("ke menolak: %d %s", e.Status, e.Raw)
	}
	if row := st.surveyRow(t, r.SubmissionID); *row.Consent || len(row.Answers) != 7 {
		t.Fatalf("menolak: %+v", row)
	}
	var attached int
	_ = st.pool.QueryRow(context.Background(), `SELECT count(*) FROM photos WHERE submission_id = $1 AND status = 'attached'`, r.SubmissionID).Scan(&attached)
	if attached != 0 {
		t.Fatalf("foto attached = %d, ingin 0", attached)
	}
}
