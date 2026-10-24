package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/adapter/wanotif"
	"photo-annotated/internal/testutil/wastub"
	"photo-annotated/internal/usecase"
)

const (
	testSuperadminInput = "0813-1111-2222"
	testSuperadmin      = "6281311112222"
	testAdminSecret     = "admin-secret-0123456789abcdef0123456789"
	testAdminTemplate   = "*{code}* kode masuk ADMIN"
)

func newTestAdminLogin(otps usecase.OTPRepo, codes usecase.OTPCodes, channel *usecase.Channel, phones *usecase.PhoneLimiter) *usecase.AdminLogin {
	tokens, err := signer.NewAdminTokens(testAdminSecret, testSuperadmin)
	if err != nil {
		panic(err)
	}
	return usecase.NewAdminLogin(otps, codes, channel, phones, tokens, usecase.SystemClock{}, usecase.AdminLoginConfig{
		OTP: usecase.OTPRequestConfig{ResendCooldown: time.Minute, MaxQueue: 200, DailyGlobalCap: 500, SendInterval: 1500 * time.Millisecond, CodeTTL: 5 * time.Minute},
		MaxAttempts: 5, TokenTTL: 8 * time.Hour, Name: "Super Admin", Phone: testSuperadmin,
	})
}

// testAdmin merakit login superadmin untuk stack mode none: antrean OTP, dispatcher, dan stub
// wa-notif sendiri (seperti cmd/api yang menjalankan dispatcher di mode none bila admin aktif).
func testAdmin(t *testing.T) func(*pgxpool.Pool) adminParts {
	return func(pool *pgxpool.Pool) adminParts {
		codes, err := signer.NewOTPCodes("pepper-0123456789abcdef0123456789abcdef", 6)
		if err != nil {
			t.Fatal(err)
		}
		wa := wastub.NewServer(t, wastub.Options{})
		sender, err := wanotif.New(wa.URL, "", 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		otps := postgres.NewOTPRepo(pool)
		channel := usecase.NewChannel(3)
		phones := usecase.NewPhoneLimiter(time.Nanosecond, 100)
		disp := usecase.NewOTPDispatcher(otps, sender, codes, channel, phones, usecase.SystemClock{}, usecase.OTPDispatchConfig{
			CodeTTL: 5 * time.Minute, MessageTemplate: "*{code}* kode Anda", AdminMessageTemplate: testAdminTemplate,
			RetryDelays: []time.Duration{3 * time.Second}, StuckAfter: time.Minute,
		})
		return adminParts{login: newTestAdminLogin(otps, codes, channel, phones), channel: channel, disp: disp, wa: wa}
	}
}

// adminToken login sebagai superadmin lewat OTP (request → kirim lewat stub → verify).
func (s *stack) adminToken(t *testing.T) string {
	t.Helper()
	resp, m := s.postJSON(t, "/api/v1/admin/otp/request", `{"phone":"`+testSuperadminInput+`","turnstile_token":"human"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("admin otp/request: %d %v", resp.StatusCode, m)
	}
	if res, err := s.admin.disp.DispatchOnce(context.Background()); err != nil || res.Outcome != usecase.DispatchSent {
		t.Fatalf("dispatch: %+v %v", res, err)
	}
	resp, v := s.postJSON(t, "/api/v1/admin/otp/verify", `{"challenge_id":"`+m["challenge_id"].(string)+`","code":"`+s.admin.wa.LastCodeFor(testSuperadmin)+`"}`)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || v["expires_at"] == nil {
		t.Fatalf("admin verify: %d %v", resp.StatusCode, v)
	}
	adm, _ := v["admin"].(map[string]any)
	if adm["name"] != "Super Admin" || adm["phone"] != "62813****2222" {
		t.Fatalf("profil admin: %v", v)
	}
	tok := v["access_token"].(string)
	if !strings.HasPrefix(tok, "adm2.") {
		t.Fatalf("token = %q", tok)
	}
	return tok
}

func (s *stack) adminLogin(t *testing.T) *stack {
	t.Helper()
	return s.with(map[string]string{"Authorization": "Bearer " + s.adminToken(t)})
}

func TestE2EAdminModeNone(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	sub := st.submitted(t, 1)

	// login username/password lama sudah dihapus
	if resp, _ := st.postJSON(t, "/api/v1/admin/login", `{"username":"admin","password":"rahasia-admin"}`); resp.StatusCode != 404 {
		t.Fatalf("/admin/login: %d", resp.StatusCode)
	}
	admin := st.adminLogin(t)
	resp, m := admin.authed(t, "GET", "/api/v1/admin/me", "")
	if resp.StatusCode != 200 || m["auth_mode"] != "none" || m["name"] != "Super Admin" || m["phone"] != "62813****2222" ||
		m["expires_at"] == nil || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("admin/me: %d %v", resp.StatusCode, m)
	}
	// halaman login admin memakai channel-status miliknya sendiri (mode none tidak punya /auth/*)
	if resp, m := st.getJSON(t, "/api/v1/admin/channel-status"); resp.StatusCode != 200 || m["otp_available"] != true {
		t.Fatalf("admin/channel-status: %d %v", resp.StatusCode, m)
	}

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
	junk := st.with(map[string]string{"Authorization": "Bearer adm2.99999999999.AAAA"})
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
	for _, p := range []string{"/api/v1/admin/otp/request", "/api/v1/admin/otp/verify", "/api/v1/admin/login"} {
		if resp, _ := off.postJSON(t, p, `{"phone":"`+testSuperadminInput+`"}`); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("admin dimatikan %s: %d", p, resp.StatusCode)
		}
	}
	for _, p := range []string{"/api/v1/admin/me", "/api/v1/admin/channel-status", "/api/v1/admin/otp/" + uuid.NewString()} {
		if resp, _ := off.getJSON(t, p); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("admin dimatikan %s: %d", p, resp.StatusCode)
		}
	}
	// token admin yang sah dari instance lain tidak dikenali saat admin dimatikan
	on := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 2})
	tok := on.adminToken(t) // 1 permintaan kode
	sub := off.submitted(t, 1)
	if code, _, _ := off.get(t, "/api/v1/submissions/"+sub.ID.String(), map[string]string{"Authorization": "Bearer " + tok}); code != 401 {
		t.Fatalf("token admin saat admin dimatikan: %d", code)
	}
	// request dan resend berbagi satu batas per IP (admin_login)
	on.postJSON(t, "/api/v1/admin/otp/resend", `{"challenge_id":"`+uuid.NewString()+`"}`)
	resp, m := on.postJSON(t, "/api/v1/admin/otp/request", `{"phone":"`+testSuperadminInput+`"}`)
	if resp.StatusCode != http.StatusTooManyRequests || errField(m) != "RATE_LIMITED" {
		t.Fatalf("rate limit login admin: %d %v", resp.StatusCode, m)
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

// Nomor selain nomor superadmin mendapat respons dan perilaku polling/verify yang sama, tanpa pesan.
func TestE2EAdminLoginAntiEnumeration(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)

	// nomor palsu lebih dulu: antrean masih kosong sehingga "terkirim" setelah 1,5 detik
	resp, fake := s.postJSON(t, "/api/v1/admin/otp/request", `{"phone":"081399998888","turnstile_token":"human"}`)
	_, real := s.postJSON(t, "/api/v1/admin/otp/request", `{"phone":"`+testSuperadminInput+`","turnstile_token":"human"}`)
	if resp.StatusCode != 202 || len(fake) != len(real) || fake["status"] != "queued" || real["status"] != "queued" ||
		fake["resend_after_seconds"] != real["resend_after_seconds"] {
		t.Fatalf("respons beda: asli=%v palsu=%v", real, fake)
	}
	// Turnstile wajib bila aktif
	if resp, m := s.postJSON(t, "/api/v1/admin/otp/request", `{"phone":"`+testSuperadminInput+`","turnstile_token":"bot"}`); resp.StatusCode != 403 || errField(m) != "TURNSTILE_FAILED" {
		t.Fatalf("turnstile: %d %v", resp.StatusCode, m)
	}
	realID, fakeID := real["challenge_id"].(string), fake["challenge_id"].(string)
	for _, id := range []string{realID, fakeID} {
		if _, m := s.getJSON(t, "/api/v1/admin/otp/"+id); m["status"] != "queued" {
			t.Fatalf("status awal %s: %v", id, m)
		}
	}
	if res, err := s.disp.DispatchOnce(context.Background()); err != nil || res.Outcome != usecase.DispatchSent {
		t.Fatalf("dispatch: %+v %v", res, err)
	}
	if res, _ := s.disp.DispatchOnce(context.Background()); res.Outcome != usecase.DispatchIdle {
		t.Fatalf("nomor bukan superadmin tidak boleh diantrekan: %+v", res)
	}
	msgs := s.wa.Messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "ADMIN") {
		t.Fatalf("pesan WhatsApp = %+v", msgs)
	}
	time.Sleep(1600 * time.Millisecond) // challenge palsu "terkirim" setelah perkiraan tunggu
	for _, id := range []string{realID, fakeID} {
		resp, m := s.getJSON(t, "/api/v1/admin/otp/"+id)
		if resp.StatusCode != 200 || m["status"] != "sent" || m["expires_at"] == nil {
			t.Fatalf("status setelah kirim %s: %v", id, m)
		}
	}
	code := s.wa.LastCodeFor(testSuperadmin)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for _, id := range []string{realID, fakeID} {
		resp, m := s.postJSON(t, "/api/v1/admin/otp/verify", `{"challenge_id":"`+id+`","code":"`+wrong+`"}`)
		det, _ := m["error"].(map[string]any)["details"].(map[string]any)
		if resp.StatusCode != 422 || errField(m) != "OTP_INVALID" || det["attempts_left"] != 4.0 {
			t.Fatalf("kode salah %s: %d %v", id, resp.StatusCode, m)
		}
	}
	// challenge admin tidak bisa dipakai di alur pengguna, dan sebaliknya tidak terlihat
	if resp, m := s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+realID+`","code":"`+code+`"}`); resp.StatusCode != 410 || errField(m) != "OTP_EXPIRED" {
		t.Fatalf("challenge admin di /auth/otp/verify: %d %v", resp.StatusCode, m)
	}
	if _, m := s.getJSON(t, "/api/v1/auth/otp/"+realID); m["status"] != "queued" {
		t.Fatalf("challenge admin di /auth/otp: %v", m)
	}
	if resp, m := s.postJSON(t, "/api/v1/admin/otp/verify", `{"challenge_id":"`+realID+`","code":"`+code+`"}`); resp.StatusCode != 200 {
		t.Fatalf("verify admin: %d %v", resp.StatusCode, m)
	}
	// kode hanya sekali pakai
	if resp, m := s.postJSON(t, "/api/v1/admin/otp/verify", `{"challenge_id":"`+realID+`","code":"`+code+`"}`); resp.StatusCode != 410 || errField(m) != "OTP_EXPIRED" {
		t.Fatalf("verify ulang: %d %v", resp.StatusCode, m)
	}
}

// Login admin dan login pengguna pada nomor yang sama terpisah: challenge tidak saling membatalkan,
// token pengguna bukan token admin, dan sebaliknya.
func TestE2EAdminAndUserSeparate(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0, func(o *stackOpts) { o.otpCooldown = time.Nanosecond })
	user := s.login(t, testSuperadminInput, testSuperadmin) // superadmin juga punya akun pengguna

	// kode reset pengguna yang sedang berjalan tidak dibatalkan oleh login admin
	_, reset := s.postJSON(t, "/api/v1/auth/password/forgot", `{"phone":"`+testSuperadminInput+`","turnstile_token":"human"}`)
	if res, err := s.disp.DispatchOnce(context.Background()); err != nil || res.Outcome != usecase.DispatchSent {
		t.Fatalf("dispatch reset: %+v %v", res, err)
	}
	admin := s.adminLogin(t)
	var status string
	if err := s.pool.QueryRow(context.Background(), `SELECT status FROM otp_challenges WHERE id = $1`, reset["challenge_id"]).Scan(&status); err != nil || status != "sent" {
		t.Fatalf("challenge reset pengguna: %q %v", status, err)
	}

	if resp, m := user.authed(t, "GET", "/api/v1/admin/me", ""); resp.StatusCode != 401 {
		t.Fatalf("token pengguna di /admin/me: %d %v", resp.StatusCode, m)
	}
	if resp, m := admin.authed(t, "GET", "/api/v1/auth/me", ""); resp.StatusCode != 403 || errField(m) != "ADMIN_READ_ONLY" {
		t.Fatalf("token admin di /auth/me: %d %v", resp.StatusCode, m)
	}
	if resp, m := admin.authed(t, "GET", "/api/v1/admin/me", ""); resp.StatusCode != 200 || m["auth_mode"] != "otp" {
		t.Fatalf("admin/me: %d %v", resp.StatusCode, m)
	}
	if resp, _ := s.authed(t, "GET", "/api/v1/admin/me", ""); resp.StatusCode != 401 {
		t.Fatalf("admin/me tanpa token: %d", resp.StatusCode)
	}
}
