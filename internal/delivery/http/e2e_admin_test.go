package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/usecase"
)

func testAdmin(t *testing.T) *usecase.AdminAuth {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("rahasia-admin"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := signer.NewAdminTokens("admin-secret-0123456789abcdef0123456789")
	return usecase.NewAdminAuth(tokens, usecase.SystemClock{}, usecase.AdminConfig{Username: "admin", PasswordBcrypt: string(hash), TokenTTL: 8 * time.Hour})
}

func (s *stack) adminLogin(t *testing.T) *stack {
	t.Helper()
	resp, m := s.postJSON(t, "/api/v1/admin/login", `{"username":"admin","password":"rahasia-admin"}`)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || m["expires_at"] == nil {
		t.Fatalf("admin login: %d %v", resp.StatusCode, m)
	}
	return s.with(map[string]string{"Authorization": "Bearer " + m["access_token"].(string)})
}

func TestE2EAdminModeNone(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	sub := st.submitted(t, 1)

	for _, body := range []string{`{"username":"admin","password":"salah"}`, `{"username":"root","password":"rahasia-admin"}`} {
		resp, m := st.postJSON(t, "/api/v1/admin/login", body)
		if resp.StatusCode != 401 || errField(m) != "INVALID_CREDENTIALS" {
			t.Fatalf("%s: %d %v", body, resp.StatusCode, m)
		}
	}
	admin := st.adminLogin(t)

	// baca tanpa edit_token
	if code, _, body := admin.get(t, "/api/v1/submissions/"+sub.ID.String(), nil); code != 200 {
		t.Fatalf("admin baca: %d %s", code, body)
	}
	if code, _, _ := admin.get(t, "/api/v1/photos/"+sub.Photo[0].String()+"/url", nil); code != 200 {
		t.Fatalf("admin url foto: %d", code)
	}
	if code, _, _ := admin.get(t, "/api/v1/submissions/"+uuid.NewString(), nil); code != 404 {
		t.Fatalf("admin baca id tak ada: %d", code)
	}
	// hanya baca: edit, presign, submit → 403 (walau membawa edit_token yang sah)
	if r := admin.edit(t, sub.ID, sub.Token, editBody(1, sub.Photo...)); r.Status != 403 || r.Code != "ADMIN_READ_ONLY" {
		t.Fatalf("admin edit: %d %s", r.Status, r.Raw)
	}
	if resp, m := admin.postJSON(t, "/api/v1/uploads/presign", `{"content_type":"image/webp","size_bytes":1000,"width":10,"height":10}`); resp.StatusCode != 403 || errField(m) != "ADMIN_READ_ONLY" {
		t.Fatalf("admin presign: %d %v", resp.StatusCode, m)
	}
	if r := admin.submit(t, submitBody(uuid.New(), sub.Photo[0])); r.Status != 403 {
		t.Fatalf("admin submit: %d", r.Status)
	}
	// token asal-asalan pada mode none → 401, bukan diabaikan
	junk := st.with(map[string]string{"Authorization": "Bearer adm1.99999999999.AAAA"})
	if code, _, _ := junk.get(t, "/api/v1/submissions/"+sub.ID.String(), map[string]string{"X-Edit-Token": sub.Token}); code != 401 {
		t.Fatalf("token palsu: %d", code)
	}
	// pemilik anonim tetap berjalan
	if r := st.edit(t, sub.ID, sub.Token, editBody(1, sub.Photo...)); r.Status != 200 {
		t.Fatalf("edit pemilik: %d %s", r.Status, r.Raw)
	}
}

func TestE2EAdminDisabledAndRateLimited(t *testing.T) {
	off := newStack(t, stackOpts{})
	if resp, _ := off.postJSON(t, "/api/v1/admin/login", `{"username":"admin","password":"rahasia-admin"}`); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("admin dimatikan: %d", resp.StatusCode)
	}
	// token admin yang sah dari instance lain tidak dikenali saat admin dimatikan
	on := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 2})
	adm := on.adminLogin(t)
	tok := adm.client.Transport.(headerTransport).h["Authorization"]
	sub := off.submitted(t, 1)
	if code, _, _ := off.get(t, "/api/v1/submissions/"+sub.ID.String(), map[string]string{"Authorization": tok}); code != 401 {
		t.Fatalf("token admin saat admin dimatikan: %d", code)
	}
	on.postJSON(t, "/api/v1/admin/login", `{"username":"admin","password":"salah"}`)
	resp, m := on.postJSON(t, "/api/v1/admin/login", `{"username":"admin","password":"rahasia-admin"}`)
	if resp.StatusCode != http.StatusTooManyRequests || errField(m) != "RATE_LIMITED" {
		t.Fatalf("rate limit login: %d %v", resp.StatusCode, m)
	}
}

func TestE2EAdminModeOTP(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	alice := s.login(t, "081200000001", "6281200000001")
	sub := alice.submitted(t, 1)

	admin := s.adminLogin(t)

	if code, _, body := admin.get(t, "/api/v1/submissions/"+sub.ID.String(), nil); code != 200 {
		t.Fatalf("admin baca submission pengguna: %d %s", code, body)
	}
	if code, _, _ := admin.get(t, "/api/v1/photos/"+sub.Photo[0].String()+"/url", nil); code != 200 {
		t.Fatalf("admin url foto: %d", code)
	}
	if r := admin.edit(t, sub.ID, "", editBody(1, sub.Photo...)); r.Status != 403 || r.Code != "ADMIN_READ_ONLY" {
		t.Fatalf("admin edit: %d %s", r.Status, r.Raw)
	}
	if resp, m := admin.postJSON(t, "/api/v1/uploads/presign", `{"content_type":"image/webp","size_bytes":1000,"width":10,"height":10}`); resp.StatusCode != 403 {
		t.Fatalf("admin presign: %d %v", resp.StatusCode, m)
	}
	if resp, m := admin.authed(t, "GET", "/api/v1/auth/me", ""); resp.StatusCode != 403 {
		t.Fatalf("admin /auth/me: %d %v", resp.StatusCode, m)
	}
	// pengguna biasa tetap tidak bisa membaca milik orang lain
	bob := s.login(t, "081200000002", "6281200000002")
	if code, _, _ := bob.get(t, "/api/v1/submissions/"+sub.ID.String(), nil); code != 404 {
		t.Fatalf("bob: %d", code)
	}
}

func TestE2EAdminExport(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	a := st.submitted(t, 3)
	b := st.submitted(t, 2)
	admin := st.adminLogin(t)

	resp, err := admin.client.Get(st.srv.URL + "/api/v1/admin/export?include_urls=true")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/x-ndjson" ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), ".jsonl") {
		t.Fatalf("export: %d %v", resp.StatusCode, resp.Header)
	}
	if resp.Trailer.Get("X-Export-Complete") != "true" || resp.Trailer.Get("X-Export-Rows") != "5" {
		t.Fatalf("trailer = %v", resp.Trailer)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d baris", len(lines))
	}
	// URL di export benar-benar bisa dipakai untuk mengunduh foto aslinya
	var row struct {
		PhotoID string `json:"photo_id"`
		Image   struct {
			URL string `json:"url"`
		} `json:"image"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatal(err)
	}
	code, _, img := st.get(t, row.Image.URL, nil)
	want := map[string][]byte{}
	for _, s := range []e2eSubmission{a, b} {
		for i, id := range s.Photo {
			want[id.String()] = s.Body[i]
		}
	}
	if code != 200 || !bytes.Equal(img, want[row.PhotoID]) {
		t.Fatalf("unduh dari URL export: %d (%d byte)", code, len(img))
	}

	// akses & validasi
	if code, _, _ := st.get(t, "/api/v1/admin/export", nil); code != 401 {
		t.Fatalf("tanpa token: %d", code)
	}
	if code, _, _ := st.get(t, "/api/v1/admin/export", map[string]string{"X-Edit-Token": a.Token}); code != 401 {
		t.Fatalf("edit token: %d", code)
	}
	for _, q := range []string{"format=csv", "from=kemarin", "include_urls=mungkin", "from=2026-10-25T00:00:00Z&to=2026-10-24T00:00:00Z"} {
		code, _, body := admin.get(t, "/api/v1/admin/export?"+q, nil)
		if code != 422 {
			t.Fatalf("%s: %d %s", q, code, body)
		}
	}
	code, _, body = admin.get(t, "/api/v1/admin/export?from=2099-01-01T00:00:00Z", nil)
	if code != 200 || len(body) != 0 {
		t.Fatalf("rentang kosong: %d %q", code, body)
	}
}
