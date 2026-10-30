package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"photo-annotated/internal/usecase"
)

func regWith(phone, nim, group string) string {
	return `{"name":"Mahasiswa","email":"m@example.com","phone":"` + phone + `","password":"rahasia123","nim":"` + nim +
		`","group_id":` + group + `,"turnstile_token":"human"}`
}

// verifyReg mengirim kode challenge registrasi lewat stub lalu memverifikasinya.
func (s *authStack) verifyReg(t *testing.T, challengeID, canonical string) (*http.Response, map[string]any) {
	t.Helper()
	return s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+challengeID+`","code":"`+s.wa.LastCodeFor(canonical)+`"}`)
}

func (s *authStack) dispatchAll(t *testing.T) {
	t.Helper()
	for {
		res, err := s.disp.DispatchOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != usecase.DispatchSent {
			return
		}
	}
}

// spec-pendataan §5.4: NIM wajib + unik, kelompok aktif wajib; /auth/me memuat nim + group.
func TestE2ERegisterWithNIMAndGroup(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)

	for _, tc := range []struct {
		name, body string
		code       string
	}{
		{"tanpa nim", `{"name":"A","email":"a@example.com","phone":"081200000009","password":"rahasia123","group_id":1,"turnstile_token":"human"}`, "INVALID_NIM"},
		{"nim bersimbol", regWith("081200000009", "251-FS-01", "1"), "INVALID_NIM"},
		{"tanpa kelompok", `{"name":"A","email":"a@example.com","phone":"081200000009","password":"rahasia123","nim":"251FS01017","turnstile_token":"human"}`, "INVALID_GROUP"},
		{"kelompok tidak ada", regWith("081200000009", "251FS01017", "999"), "INVALID_GROUP"},
	} {
		if resp, m := s.postJSON(t, "/api/v1/auth/register", tc.body); resp.StatusCode != 422 || errField(m) != tc.code {
			t.Fatalf("%s: %d %v", tc.name, resp.StatusCode, m)
		}
	}

	// NIM huruf kecil disimpan huruf besar; me/login memuat nim + group
	_, reg := s.postJSON(t, "/api/v1/auth/register", regWith("081200000001", " 251fs01017 ", "1"))
	s.dispatchAll(t)
	resp, v := s.verifyReg(t, reg["challenge_id"].(string), "6281200000001")
	u, _ := v["user"].(map[string]any)
	if g, _ := u["group"].(map[string]any); resp.StatusCode != 200 || u["nim"] != "251FS01017" || g["id"] != 1.0 || g["name"] != "Kelompok Uji" {
		t.Fatalf("verify: %d %v", resp.StatusCode, v)
	}
	_, me := s.authed(t, "GET", "/api/v1/auth/me", "Bearer "+v["access_token"].(string))
	if me["nim"] != "251FS01017" || me["group"].(map[string]any)["name"] != "Kelompok Uji" {
		t.Fatalf("me: %v", me)
	}
	resp, login := s.postJSON(t, "/api/v1/auth/login", `{"phone":"081200000001","password":"rahasia123"}`)
	if resp.StatusCode != 200 || login["user"].(map[string]any)["nim"] != "251FS01017" {
		t.Fatalf("login: %d %v", resp.StatusCode, login)
	}

	// NIM akun aktif → 409 (beda huruf besar/kecil tetap sama)
	if resp, m := s.postJSON(t, "/api/v1/auth/register", regWith("081200000002", "251Fs01017", "1")); resp.StatusCode != 409 || errField(m) != "NIM_TAKEN" {
		t.Fatalf("nim terpakai: %d %v", resp.StatusCode, m)
	}

	// kelompok arsip → 422; kelompok terpakai tidak bisa dihapus; user_count terisi
	admin := s.adminLogin(t)
	_, arch := admin.send(t, "POST", "/api/v1/admin/groups", `{"name":"Kelompok Arsip"}`)
	admin.send(t, "PUT", "/api/v1/admin/groups/"+idOf(arch), `{"name":"Kelompok Arsip","archived":true}`)
	if resp, m := s.postJSON(t, "/api/v1/auth/register", regWith("081200000003", "251FS09999", idOf(arch))); resp.StatusCode != 422 || errField(m) != "INVALID_GROUP" {
		t.Fatalf("kelompok arsip: %d %v", resp.StatusCode, m)
	}
	if resp, m := admin.send(t, "DELETE", "/api/v1/admin/groups/1", ""); resp.StatusCode != 409 || errField(m) != "GROUP_IN_USE" {
		t.Fatalf("hapus kelompok terpakai: %d %v", resp.StatusCode, m)
	}
	_, gs := admin.getJSON(t, "/api/v1/admin/groups")
	if l := groupList(t, gs); l[0]["name"] != "Kelompok Uji" || l[0]["user_count"] != 1.0 {
		t.Fatalf("user_count: %v", l)
	}

	// admin: daftar/detail memuat nim + group; pindah kelompok
	_, users := admin.getJSON(t, "/api/v1/admin/users")
	item := users["items"].([]any)[0].(map[string]any)
	if item["nim"] != "251FS01017" || item["group"].(map[string]any)["id"] != 1.0 {
		t.Fatalf("admin/users: %v", item)
	}
	uid := item["id"].(string)
	_, k2 := admin.send(t, "POST", "/api/v1/admin/groups", `{"name":"Kelompok 2"}`)
	for _, tc := range []struct {
		path, body string
		status     int
		code       string
	}{
		{"/api/v1/admin/users/" + uid + "/group", `{"group_id":` + idOf(arch) + `}`, 422, "INVALID_GROUP"},
		{"/api/v1/admin/users/" + uid + "/group", `{"group_id":999}`, 422, "INVALID_GROUP"},
		{"/api/v1/admin/users/" + uid + "/group", `{}`, 422, "INVALID_GROUP"},
		{"/api/v1/admin/users/00000000-0000-0000-0000-000000000000/group", `{"group_id":` + idOf(k2) + `}`, 404, "NOT_FOUND"},
	} {
		if resp, m := admin.send(t, "PUT", tc.path, tc.body); resp.StatusCode != tc.status || errField(m) != tc.code {
			t.Fatalf("%s %s: %d %v", tc.path, tc.body, resp.StatusCode, m)
		}
	}
	resp, m := admin.send(t, "PUT", "/api/v1/admin/users/"+uid+"/group", `{"group_id":`+idOf(k2)+`}`)
	if resp.StatusCode != 200 || m["group"].(map[string]any)["name"] != "Kelompok 2" {
		t.Fatalf("pindah kelompok: %d %v", resp.StatusCode, m)
	}
	if resp, _ := s.send(t, "PUT", "/api/v1/admin/users/"+uid+"/group", `{"group_id":1}`); resp.StatusCode != 401 {
		t.Fatalf("tanpa token admin: %d", resp.StatusCode)
	}
	// kelompok lama kini kosong → boleh dihapus
	if resp, _ := admin.send(t, "DELETE", "/api/v1/admin/groups/1", ""); resp.StatusCode != 204 {
		t.Fatalf("hapus kelompok kosong: %d", resp.StatusCode)
	}
}

// Dua pendaftar menunggu dengan NIM sama: yang memverifikasi belakangan → 409 NIM_TAKEN dan challenge
// ditutup (kode yang sama tidak bisa dipakai lagi).
func TestE2ENIMRaceSecondVerifierLoses(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	_, a := s.postJSON(t, "/api/v1/auth/register", regWith("081200000001", "251FS01017", "1"))
	_, b := s.postJSON(t, "/api/v1/auth/register", regWith("081200000002", "251FS01017", "1"))
	if a["challenge_id"] == nil || b["challenge_id"] == nil {
		t.Fatalf("keduanya harus diterima saat register: %v %v", a, b)
	}
	s.dispatchAll(t)
	if resp, m := s.verifyReg(t, a["challenge_id"].(string), "6281200000001"); resp.StatusCode != 200 {
		t.Fatalf("pertama: %d %v", resp.StatusCode, m)
	}
	if resp, m := s.verifyReg(t, b["challenge_id"].(string), "6281200000002"); resp.StatusCode != 409 || errField(m) != "NIM_TAKEN" {
		t.Fatalf("kedua: %d %v", resp.StatusCode, m)
	}
	if resp, m := s.verifyReg(t, b["challenge_id"].(string), "6281200000002"); resp.StatusCode != 410 || errField(m) != "OTP_EXPIRED" {
		t.Fatalf("challenge kedua harus tertutup: %d %v", resp.StatusCode, m)
	}
	var status string
	if err := s.pool.QueryRow(context.Background(), `SELECT status FROM users WHERE phone = '6281200000002'`).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("pendaftar kedua tetap pending: %q %v", status, err)
	}
}
