package httpapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/usecase"
)

func TestE2EAdminUsersBlockAndDeleteData(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0, func(o *stackOpts) { o.otpCooldown = time.Nanosecond })
	alice := s.login(t, "081200000001", "6281200000001") // password rahasia123
	bob := s.login(t, "085700000002", "6285700000002")
	keep := alice.submitted(t, 1)
	drop := alice.submitted(t, 2)
	bob.submitted(t, 1)
	admin := s.adminLogin(t)

	// daftar + statistik
	_, m := admin.getJSON(t, "/api/v1/admin/users?q=0812")
	items, _ := m["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("cari nomor: %v", m)
	}
	u := items[0].(map[string]any)
	aliceID := u["id"].(string)
	st := u["stats"].(map[string]any)
	if u["phone"] != "6281200000001" || u["blocked"] != false || u["blocked_reason"] != nil || st["submissions"] != 2.0 ||
		st["annotations"] != 4.0 || st["pending"] != 2.0 || st["last_submission_at"] == nil || u["last_login_at"] == nil {
		t.Fatalf("annotator: %v", u)
	}
	if _, m := admin.getJSON(t, "/api/v1/admin/users"); len(m["items"].([]any)) != 2 || m["next_cursor"] != nil {
		t.Fatalf("semua: %v", m)
	}
	p1 := admin.adminUsersPage(t, "?limit=1")
	if len(p1.items) != 1 || p1.next == "" {
		t.Fatalf("halaman 1: %+v", p1)
	}
	if p2 := admin.adminUsersPage(t, "?limit=1&cursor="+p1.next); len(p2.items) != 1 || p2.next != "" || p2.items[0] == p1.items[0] {
		t.Fatalf("halaman 2: %+v", p2)
	}
	for _, q := range []string{"status=semua", "limit=0", "cursor=x"} {
		if code, _, _ := admin.get(t, "/api/v1/admin/users?"+q, nil); code != 422 {
			t.Fatalf("%s: %d", q, code)
		}
	}

	// blokir: alasan wajib; sesi aktif langsung 401
	if resp, m := admin.send(t, "POST", "/api/v1/admin/users/"+aliceID+"/block", `{"reason":""}`); resp.StatusCode != 422 || errField(m) != "INVALID_BLOCK_REASON" {
		t.Fatalf("blokir tanpa alasan: %d %v", resp.StatusCode, m)
	}
	resp, m := admin.send(t, "POST", "/api/v1/admin/users/"+aliceID+"/block", `{"reason":"Anotasi asal-asalan berulang kali"}`)
	if resp.StatusCode != 200 || m["blocked"] != true || m["blocked_reason"] != "Anotasi asal-asalan berulang kali" || m["blocked_at"] == nil {
		t.Fatalf("blokir: %d %v", resp.StatusCode, m)
	}
	if resp, _ := admin.send(t, "POST", "/api/v1/admin/users/"+aliceID+"/block", `{"reason":"lagi"}`); resp.StatusCode != 200 {
		t.Fatalf("blokir ulang: %d", resp.StatusCode)
	}
	if code, _, _ := alice.get(t, "/api/v1/submissions/"+keep.ID.String(), nil); code != 401 {
		t.Fatalf("sesi pengguna diblokir: %d", code)
	}
	// login: password benar → 403, salah → 401 biasa
	if resp, m := s.postJSON(t, "/api/v1/auth/login", `{"phone":"081200000001","password":"rahasia123"}`); resp.StatusCode != 403 || errField(m) != "ACCOUNT_BLOCKED" {
		t.Fatalf("login diblokir: %d %v", resp.StatusCode, m)
	}
	if resp, m := s.postJSON(t, "/api/v1/auth/login", `{"phone":"081200000001","password":"salahsalah"}`); resp.StatusCode != 401 || errField(m) != "INVALID_CREDENTIALS" {
		t.Fatalf("login password salah: %d %v", resp.StatusCode, m)
	}
	// register / lupa password tidak mengirim WhatsApp
	before := len(s.wa.Messages())
	for _, p := range []struct{ path, body string }{
		{"/api/v1/auth/password/forgot", `{"phone":"081200000001","turnstile_token":"human"}`},
		{"/api/v1/auth/register", `{"name":"A","email":"a@example.com","phone":"081200000001","password":"rahasia123","nim":"BARU00001","group_id":1,"turnstile_token":"human"}`},
	} {
		if resp, m := s.postJSON(t, p.path, p.body); resp.StatusCode != 202 {
			t.Fatalf("%s: %d %v", p.path, resp.StatusCode, m)
		}
	}
	if res, _ := s.disp.DispatchOnce(context.Background()); res.Outcome != usecase.DispatchIdle || len(s.wa.Messages()) != before {
		t.Fatalf("tidak boleh ada pesan untuk akun diblokir: %+v", res)
	}
	if r := admin.adminList(t, "?user_id="+aliceID); len(r.Items) != 2 || !r.Items[0].Annotator.Blocked {
		t.Fatalf("daftar submission menandai annotator diblokir: %+v", r.Items)
	}
	if _, m := admin.getJSON(t, "/api/v1/admin/users?status=blocked"); len(m["items"].([]any)) != 1 {
		t.Fatalf("filter blocked: %v", m)
	}

	// buka blokir: login dengan password lama lagi
	if resp, m := admin.send(t, "POST", "/api/v1/admin/users/"+aliceID+"/unblock", ""); resp.StatusCode != 200 || m["blocked"] != false {
		t.Fatalf("buka blokir: %d %v", resp.StatusCode, m)
	}
	if resp, m := s.postJSON(t, "/api/v1/auth/login", `{"phone":"081200000001","password":"rahasia123"}`); resp.StatusCode != 200 {
		t.Fatalf("login setelah buka blokir: %d %v", resp.StatusCode, m)
	}

	// hapus data: yang accepted dipertahankan
	if resp, _ := admin.send(t, "PUT", "/api/v1/admin/submissions/"+keep.ID.String()+"/review", `{"status":"accepted","version":1}`); resp.StatusCode != 200 {
		t.Fatal("terima")
	}
	keys := []string{s.storageKey(t, drop.Photo[0]), s.storageKey(t, drop.Photo[1])}
	resp, m = admin.send(t, "POST", "/api/v1/admin/users/"+aliceID+"/delete-data", `{"include_accepted":false}`)
	if resp.StatusCode != 202 || m["submissions"] != 1.0 || m["photos"] != 2.0 || m["annotations"] != 2.0 || m["status"] != "cleanup_pending" {
		t.Fatalf("hapus data: %d %v", resp.StatusCode, m)
	}
	if s.cleanupRequests.Load() != 1 {
		t.Fatal("hapus data harus meminta cleanup segera")
	}
	if r := admin.adminList(t, "?user_id="+aliceID); len(r.Items) != 1 || r.Items[0].ID != keep.ID.String() {
		t.Fatalf("yang accepted harus tetap: %+v", r.Items)
	}
	cu := usecase.NewCleanup(postgres.NewCleanupRepo(s.pool), s.store, usecase.SystemClock{}, usecase.CleanupConfig{
		RetentionDays: 90, PendingTTL: 24 * time.Hour, Batch: 100, TmpMaxAge: time.Hour, OrphanMinAge: 24 * time.Hour})
	if rep, err := cu.RunOnce(context.Background()); err != nil || rep.FilesDeleted < 2 || rep.SubmissionsDeleted != 1 {
		t.Fatalf("cleanup: %+v %v", rep, err)
	}
	for _, k := range keys {
		if s.onDisk(k) {
			t.Fatal("file data annotator harus hilang setelah cleanup")
		}
	}
	resp, m = admin.send(t, "POST", "/api/v1/admin/users/"+aliceID+"/delete-data", `{"include_accepted":true}`)
	if resp.StatusCode != 202 || m["submissions"] != 1.0 {
		t.Fatalf("hapus data termasuk accepted: %d %v", resp.StatusCode, m)
	}
	// akun tetap ada
	if code, _, raw := admin.get(t, "/api/v1/admin/users/"+aliceID, nil); code != 200 || !strings.Contains(string(raw), `"submissions":0`) {
		t.Fatalf("akun setelah hapus data: %d %s", code, raw)
	}
	for _, p := range []string{"/api/v1/admin/users/" + uuid.NewString(), "/api/v1/admin/users/bukan-uuid"} {
		if code, _, _ := admin.get(t, p, nil); code != 404 {
			t.Fatalf("%s: %d", p, code)
		}
	}
	if resp, _ := admin.send(t, "POST", "/api/v1/admin/users/"+uuid.NewString()+"/block", `{"reason":"x"}`); resp.StatusCode != 404 {
		t.Fatalf("blokir id tak ada: %d", resp.StatusCode)
	}
	if code, _, _ := bob.get(t, "/api/v1/admin/users", nil); code != 401 {
		t.Fatalf("token pengguna: %d", code)
	}
}

type usersPage struct {
	items []string
	next  string
}

func (s *stack) adminUsersPage(t *testing.T, q string) usersPage {
	t.Helper()
	_, m := s.getJSON(t, "/api/v1/admin/users"+q)
	var p usersPage
	for _, it := range m["items"].([]any) {
		p.items = append(p.items, it.(map[string]any)["id"].(string))
	}
	if n, ok := m["next_cursor"].(string); ok {
		p.next = n
	}
	return p
}

// AC 20: pada mode none /admin/users* tidak ada.
func TestE2EAdminUsersAbsentInModeNone(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	admin := st.adminLogin(t)
	for _, p := range []string{"/api/v1/admin/users", "/api/v1/admin/users/" + uuid.NewString()} {
		if code, _, _ := admin.get(t, p, nil); code != 404 {
			t.Fatalf("%s: %d", p, code)
		}
	}
	if resp, _ := admin.send(t, "POST", "/api/v1/admin/users/"+uuid.NewString()+"/block", `{"reason":"x"}`); resp.StatusCode != 404 {
		t.Fatalf("block: %d", resp.StatusCode)
	}
}

// Hapus akun annotator: semua data (termasuk accepted dan foto yang belum dikirim) hilang setelah
// cleanup, sesi langsung 401, dan nomor yang sama bisa mendaftar lagi dengan kode WhatsApp sungguhan.
func TestE2EAdminDeleteAccount(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0, func(o *stackOpts) { o.otpCooldown = time.Nanosecond })
	alice := s.login(t, "081200000001", "6281200000001")
	bob := s.login(t, "085700000002", "6285700000002")
	accepted := alice.submitted(t, 1)
	other := alice.submitted(t, 2)
	loose := alice.uploadedPhoto(t, 7)
	kept := bob.submitted(t, 1)
	admin := s.adminLogin(t)
	_, m := admin.getJSON(t, "/api/v1/admin/users?q=0812")
	aliceID := m["items"].([]any)[0].(map[string]any)["id"].(string)
	if resp, _ := admin.send(t, "PUT", "/api/v1/admin/submissions/"+accepted.ID.String()+"/review", `{"status":"accepted","version":1}`); resp.StatusCode != 200 {
		t.Fatal("terima")
	}
	keys := []string{s.storageKey(t, accepted.Photo[0]), s.storageKey(t, other.Photo[0]), s.storageKey(t, other.Photo[1]), s.storageKey(t, loose)}

	resp, m := admin.send(t, "DELETE", "/api/v1/admin/users/"+aliceID, "")
	if resp.StatusCode != 202 || m["account_deleted"] != true || m["submissions"] != 2.0 || m["photos"] != 4.0 ||
		m["annotations"] != 4.0 || m["status"] != "cleanup_pending" {
		t.Fatalf("hapus akun: %d %v", resp.StatusCode, m)
	}
	if s.cleanupRequests.Load() != 1 {
		t.Fatal("hapus akun harus meminta cleanup segera")
	}
	if code, _, _ := alice.get(t, "/api/v1/auth/me", nil); code != 401 {
		t.Fatalf("sesi akun terhapus: %d", code)
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/users/"+aliceID, nil); code != 404 {
		t.Fatalf("akun harus hilang: %d", code)
	}
	if resp, _ := admin.send(t, "DELETE", "/api/v1/admin/users/"+aliceID, ""); resp.StatusCode != 404 {
		t.Fatalf("hapus ulang: %d", resp.StatusCode)
	}
	for _, id := range []string{accepted.ID.String(), other.ID.String()} {
		if code, _, _ := admin.get(t, "/api/v1/admin/submissions/"+id, nil); code != 404 {
			t.Fatalf("submission %s harus hilang: %d", id, code)
		}
	}
	if resp, m := s.postJSON(t, "/api/v1/auth/login", `{"phone":"081200000001","password":"rahasia123"}`); resp.StatusCode != 401 || errField(m) != "INVALID_CREDENTIALS" {
		t.Fatalf("login akun terhapus: %d %v", resp.StatusCode, m)
	}
	cu := usecase.NewCleanup(postgres.NewCleanupRepo(s.pool), s.store, usecase.SystemClock{}, usecase.CleanupConfig{
		RetentionDays: 90, PendingTTL: 24 * time.Hour, Batch: 100, TmpMaxAge: time.Hour, OrphanMinAge: 24 * time.Hour})
	if rep, err := cu.RunOnce(context.Background()); err != nil || rep.FilesDeleted < 4 || rep.SubmissionsDeleted != 2 {
		t.Fatalf("cleanup: %+v %v", rep, err)
	}
	for _, k := range keys {
		if s.onDisk(k) {
			t.Fatal("file akun terhapus harus hilang setelah cleanup")
		}
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/submissions/"+kept.ID.String(), nil); code != 200 {
		t.Fatalf("data annotator lain harus tetap: %d", code)
	}
	// nomor yang sama bisa mendaftar lagi (kode benar-benar terkirim)
	again := s.login(t, "081200000001", "6281200000001")
	if code, _, _ := again.get(t, "/api/v1/auth/me", nil); code != 200 {
		t.Fatalf("daftar ulang: %d", code)
	}
	if _, m := admin.getJSON(t, "/api/v1/admin/users?q=0812"); len(m["items"].([]any)) != 1 ||
		m["items"].([]any)[0].(map[string]any)["id"] == aliceID {
		t.Fatalf("akun baru harus berbeda: %v", m)
	}
	if resp, _ := bob.send(t, "DELETE", "/api/v1/admin/users/"+aliceID, ""); resp.StatusCode != 401 {
		t.Fatalf("token pengguna: %d", resp.StatusCode)
	}
}
