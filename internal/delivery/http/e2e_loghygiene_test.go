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

	resp, reg := s.postJSON(t, "/api/v1/auth/register", `{"name":"Rahasia Namanya","email":"surel.rahasia@example.com","phone":"0812-7777-1234","password":"PASSWORD-RAHASIA-1","turnstile_token":"human"}`)
	if resp.StatusCode != 202 {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	_, _ = s.disp.DispatchOnce(context.Background())
	code := s.wa.LastCodeFor("6281277771234")
	_, v := s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+reg["challenge_id"].(string)+`","code":"`+code+`"}`)
	token := v["access_token"].(string)
	user := s.with(map[string]string{"Authorization": "Bearer " + token})

	pid := user.uploadedPhoto(t, 0x31)
	body := `{"photos":[{"photo_id":"` + pid.String() + `","annotations":[{"text":"TEKS-ANOTASI-RAHASIA","x":0.1,"y":0.1,"w":0.2,"h":0.2}]}],"client_request_id":"` + uuid.NewString() + `"}`
	sub := user.submit(t, body)
	if sub.Status != 201 {
		t.Fatalf("submit: %d %s", sub.Status, sub.Raw)
	}
	user.edit(t, sub.SubmissionID, "", strings.Replace(body, `"client_request_id":"`, `"x":"`, 1)) // 400, body tidak dicatat
	user.edit(t, sub.SubmissionID, "", `{"version":1,"photos":[{"photo_id":"`+pid.String()+`","annotations":[{"text":"EDIT-RAHASIA","x":0.1,"y":0.1,"w":0.2,"h":0.2}]}]}`)
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
	admin.send(t, "PUT", "/api/v1/admin/labels/order", `{"ids":[]}`)
	admin.send(t, "DELETE", "/api/v1/admin/labels/"+idOf(lbl), "")
	admin.get(t, "/api/v1/admin/me", nil)
	_, fake := s.postJSON(t, "/api/v1/admin/otp/request", `{"phone":"0813-5555-6666","turnstile_token":"human"}`)
	s.postJSON(t, "/api/v1/admin/otp/verify", `{"challenge_id":"`+fake["challenge_id"].(string)+`","code":"424242"}`)
	s.getJSON(t, "/api/v1/admin/otp/"+fake["challenge_id"].(string))
	s.with(map[string]string{"Authorization": "Bearer token-palsu-rahasia"}).get(t, "/api/v1/auth/me", nil)
	s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+reg["challenge_id"].(string)+`","code":"999999"}`)
	user.authed(t, "POST", "/api/v1/auth/logout", "")
	s.get(t, "/healthz", nil)

	out := logs.String()
	if !strings.Contains(out, `"user_id"`) || !strings.Contains(out, `"code":"OTP_EXPIRED"`) ||
		!strings.Contains(out, `"msg":"admin login"`) || !strings.Contains(out, `"phone":"62813****2222"`) ||
		!strings.Contains(out, `"action":"label.create"`) || !strings.Contains(out, `"action":"label.archive"`) ||
		!strings.Contains(out, `"action":"label.reorder"`) || !strings.Contains(out, `"action":"label.delete"`) {
		t.Fatalf("log kurang lengkap (user_id/code):\n%s", out)
	}
	secrets := map[string]string{
		"kode OTP": code, "token sesi": token, "token admin": strings.TrimPrefix(adminTok, "Bearer "),
		"nomor utuh": "6281277771234", "nomor input": "0812-7777-1234", "nama": "Rahasia Namanya",
		"email": "surel.rahasia", "teks anotasi": "TEKS-ANOTASI-RAHASIA", "teks edit": "EDIT-RAHASIA",
		"kode OTP admin": adminCode, "deskripsi label": "DESKRIPSI-LABEL", "nomor superadmin": testSuperadmin, "nomor superadmin input": testSuperadminInput,
		"nomor lain di login admin": "6281355556666", "kode admin salah": "424242",
		"password daftar": "PASSWORD-RAHASIA-1", "token palsu": "token-palsu-rahasia", "tanda tangan URL": "sig=",
		"header Authorization": "Bearer", "turnstile": `"human"`,
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
