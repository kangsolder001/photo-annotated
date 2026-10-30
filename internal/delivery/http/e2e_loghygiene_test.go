package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// syncBuffer: buffer log yang aman dipakai handler HTTP berbarengan.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestE2ELogsContainNoSecrets menjalankan alur lengkap (OTP, upload, submit, edit, baca, admin,
// export, permintaan gagal) lalu memastikan log request tidak memuat rahasia maupun data pribadi
// (spec §12): kode OTP, token sesi/admin, header Authorization, tanda tangan URL, nomor HP utuh,
// nama, email, dan teks anotasi.
func TestE2ELogsContainNoSecrets(t *testing.T) {
	logs := &syncBuffer{}
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0, func(o *stackOpts) { o.logOut = logs })

	resp, reg := s.postJSON(t, "/api/v1/auth/register", `{"name":"Rahasia Namanya","email":"surel.rahasia@example.com","phone":"0812-7777-1234","password":"PASSWORD-RAHASIA-1","nim":"nimrahasia77","group_id":1,"turnstile_token":"human"}`)
	if resp.StatusCode != 202 {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	_, _ = s.disp.DispatchOnce(context.Background())
	code := s.wa.LastCodeFor("6281277771234")
	_, v := s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+reg["challenge_id"].(string)+`","code":"`+code+`"}`)
	token := v["access_token"].(string)
	user := s.with(map[string]string{"Authorization": "Bearer " + token})

	pid := user.uploadedPhoto(t, 0x31)
	// pendataan dengan data yang bisa mengenali rumah (spec-pendataan §9)
	answers := setAnswer(t, setAnswer(t, setAnswer(t, surveyYesJSON, "a_household_code", `"KODE-RUMAH-RAHASIA"`),
		"a_initials", `"Ny. Rahasiawati"`), "b_note_1", `"KETERANGAN-RAHASIA"`)
	body := `{"form_version":1,"answers":` + answers + `,"photos":[{"photo_id":"` + pid.String() + `","annotations":[{"label_id":1,"x":0.1,"y":0.1,"w":0.2,"h":0.2}]}],"client_request_id":"` + uuid.NewString() + `"}`
	sub := user.submit(t, body)
	if sub.Status != 201 {
		t.Fatalf("submit: %d %s", sub.Status, sub.Raw)
	}
	user.edit(t, sub.SubmissionID, "", strings.Replace(body, `"client_request_id":"`, `"x":"`, 1)) // 400, body tidak dicatat
	user.edit(t, sub.SubmissionID, "", `{"version":1,"form_version":1,"answers":`+setAnswer(t, answers, "c_note", `"CATATAN-OBSERVASI-RAHASIA"`)+`,"photos":[{"photo_id":"`+pid.String()+`","annotations":[{"label_id":2,"x":0.1,"y":0.1,"w":0.2,"h":0.2}]}]}`)
	user.submit(t, `{"form_version":1,"answers":`+setAnswer(t, answers, "a_initials", `"Tn. Ditolak\u0007"`)+`,"photos":[],"client_request_id":"`+uuid.NewString()+`"}`) // 422, isi tidak dicatat
	_, _, urlBody := user.get(t, "/api/v1/photos/"+pid.String()+"/url", nil)
	var pu struct{ URL string }
	_ = json.Unmarshal(urlBody, &pu)
	user.get(t, pu.URL, nil)
	user.get(t, "/api/v1/auth/me", nil)

	admin := s.adminLogin(t)
	adminTok := admin.client.Transport.(headerTransport).h["Authorization"]
	r, _ := admin.client.Get(s.srv.URL + "/api/v1/admin/export?include_urls=true")
	io.Copy(io.Discard, r.Body)
	r.Body.Close()
	adminCode := s.wa.LastCodeFor(testSuperadmin)
	_, lbl := admin.send(t, "POST", "/api/v1/admin/labels", `{"name":"Retak","description":"DESKRIPSI-LABEL"}`)
	admin.send(t, "PUT", "/api/v1/admin/labels/"+idOf(lbl), `{"name":"Retak halus","color":"#000000","archived":true}`)
	_, active := admin.getJSON(t, "/api/v1/labels")
	var order []string
	for _, l := range labelList(t, active) {
		order = append([]string{idOf(l)}, order...) // dibalik
	}
	admin.send(t, "PUT", "/api/v1/admin/labels/order", `{"ids":[`+strings.Join(order, ",")+`]}`)
	admin.send(t, "DELETE", "/api/v1/admin/labels/"+idOf(lbl), "")
	admin.get(t, "/api/v1/admin/me", nil)
	_, _, listRaw := admin.get(t, "/api/v1/admin/submissions?q=0812-7777", nil)
	if !strings.Contains(string(listRaw), "6281277771234") {
		t.Fatalf("daftar admin harus memuat nomor utuh (yang tidak boleh masuk log): %s", listRaw)
	}
	admin.get(t, "/api/v1/admin/submissions/"+sub.SubmissionID.String(), nil)
	admin.send(t, "PUT", "/api/v1/admin/submissions/"+sub.SubmissionID.String()+"/review", `{"status":"rejected","reason":"ALASAN-REVIEW-RAHASIA Budi","version":2}`)
	admin.send(t, "DELETE", "/api/v1/admin/submissions/"+sub.SubmissionID.String(), "")
	_, users := admin.getJSON(t, "/api/v1/admin/users?q=Rahasia")
	uid := users["items"].([]any)[0].(map[string]any)["id"].(string)
	admin.send(t, "POST", "/api/v1/admin/users/"+uid+"/block", `{"reason":"ALASAN-BLOKIR-RAHASIA"}`)
	admin.send(t, "POST", "/api/v1/admin/users/"+uid+"/unblock", "")
	_, grp := admin.send(t, "POST", "/api/v1/admin/groups", `{"name":"Kelompok Pindahan"}`)
	if resp, m := admin.send(t, "PUT", "/api/v1/admin/users/"+uid+"/group", `{"group_id":`+idOf(grp)+`}`); resp.StatusCode != 200 || m["nim"] != "NIMRAHASIA77" {
		t.Fatalf("pindah kelompok: %d %v", resp.StatusCode, m)
	}
	admin.send(t, "POST", "/api/v1/admin/users/"+uid+"/delete-data", `{"include_accepted":true}`)
	s.postJSON(t, "/api/v1/auth/login", `{"phone":"0812-7777-1234","password":"PASSWORD-RAHASIA-1"}`)
	_, fake := s.postJSON(t, "/api/v1/admin/otp/request", `{"phone":"0813-5555-6666","turnstile_token":"human"}`)
	s.postJSON(t, "/api/v1/admin/otp/verify", `{"challenge_id":"`+fake["challenge_id"].(string)+`","code":"424242"}`)
	s.getJSON(t, "/api/v1/admin/otp/"+fake["challenge_id"].(string))
	s.with(map[string]string{"Authorization": "Bearer token-palsu-rahasia"}).get(t, "/api/v1/auth/me", nil)
	s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+reg["challenge_id"].(string)+`","code":"999999"}`)
	user.authed(t, "POST", "/api/v1/auth/logout", "")
	admin.send(t, "DELETE", "/api/v1/admin/users/"+uid, "")
	s.get(t, "/healthz", nil)

	out := logs.String()
	if !strings.Contains(out, `"user_id"`) || !strings.Contains(out, `"code":"OTP_EXPIRED"`) ||
		!strings.Contains(out, `"msg":"admin login"`) || !strings.Contains(out, `"phone":"62813****2222"`) ||
		!strings.Contains(out, `"action":"label.create"`) || !strings.Contains(out, `"action":"label.archive"`) ||
		!strings.Contains(out, `"action":"label.reorder"`) || !strings.Contains(out, `"action":"label.delete"`) ||
		!strings.Contains(out, `"action":"submission.delete"`) || !strings.Contains(out, `"action":"submission.review"`) ||
		!strings.Contains(out, `"action":"user.block"`) || !strings.Contains(out, `"action":"user.unblock"`) ||
		!strings.Contains(out, `"action":"user.delete_data"`) || !strings.Contains(out, `"action":"group.create"`) ||
		!strings.Contains(out, `"action":"user.set_group"`) {
		t.Fatalf("log kurang lengkap (user_id/code):\n%s", out)
	}
	secrets := map[string]string{
		"kode OTP": code, "token sesi": token, "token admin": strings.TrimPrefix(adminTok, "Bearer "),
		"nomor utuh": "6281277771234", "nomor input": "0812-7777-1234", "nama": "Rahasia Namanya",
		"email":          "surel.rahasia",
		"kode OTP admin": adminCode, "alasan review": "ALASAN-REVIEW-RAHASIA", "alasan blokir": "ALASAN-BLOKIR-RAHASIA", "deskripsi label": "DESKRIPSI-LABEL", "nomor superadmin": testSuperadmin, "nomor superadmin input": testSuperadminInput,
		"nomor lain di login admin": "6281355556666", "kode admin salah": "424242",
		"password daftar": "PASSWORD-RAHASIA-1", "token palsu": "token-palsu-rahasia", "tanda tangan URL": "sig=",
		"header Authorization": "Bearer", "turnstile": `"human"`,
		"NIM": "NIMRAHASIA77", "NIM input": "nimrahasia77",
		"kode rumah tangga": "KODE-RUMAH-RAHASIA", "inisial": "Rahasiawati", "keterangan": "KETERANGAN-RAHASIA",
		"catatan observasi": "CATATAN-OBSERVASI-RAHASIA", "inisial ditolak": "Ditolak",
	}
	for what, v := range secrets {
		if v != "" && strings.Contains(out, v) {
			t.Errorf("log memuat %s (%q)", what, v)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, `"path":"/healthz"`) && !strings.Contains(line, `"level":"DEBUG"`) {
			t.Errorf("healthz sukses harus di level debug: %s", line)
		}
	}
}
