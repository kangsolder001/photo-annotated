package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/adapter/turnstile"
	"photo-annotated/internal/adapter/wanotif"
	"photo-annotated/internal/testutil/wastub"
	"photo-annotated/internal/usecase"
)

// fakeSiteverify meniru endpoint siteverify Cloudflare: token "human" lolos.
func fakeSiteverify(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, _ = w.Write([]byte(`{"success":` + map[bool]string{true: "true", false: "false"}[r.PostForm.Get("response") == "human"] + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type authStack struct {
	*stack
	channel *usecase.Channel
	disp    *usecase.OTPDispatcher
	wa      *wastub.Server
}

func newAuthStack(t *testing.T, cfURL string, otpPerMin int) *authStack {
	t.Helper()
	codes, err := signer.NewOTPCodes("pepper-0123456789abcdef0123456789abcdef", 6)
	if err != nil {
		t.Fatal(err)
	}
	as := &authStack{channel: usecase.NewChannel(3), wa: wastub.NewServer(t, wastub.Options{})}
	sender, err := wanotif.New(as.wa.URL, "", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	parts := func(pool *pgxpool.Pool) otpParts {
		users, otps := postgres.NewUserRepo(pool), postgres.NewOTPRepo(pool)
		phones := usecase.NewPhoneLimiter(time.Minute, 5)
		as.disp = usecase.NewOTPDispatcher(otps, sender, codes, as.channel, phones, usecase.SystemClock{}, usecase.OTPDispatchConfig{
			CodeTTL: 5 * time.Minute, MessageTemplate: "*{code}* kode Anda", RetryDelays: []time.Duration{3 * time.Second}, StuckAfter: time.Minute,
		})
		req := usecase.NewOTPRequest(users, otps, codes, as.channel, phones, usecase.SystemClock{},
			usecase.OTPRequestConfig{ResendCooldown: time.Minute, MaxQueue: 200, DailyGlobalCap: 500, SendInterval: 1500 * time.Millisecond, CodeTTL: 5 * time.Minute})
		sessions := postgres.NewSessionRepo(pool)
		return otpParts{
			req: req,
			verify: usecase.NewOTPVerify(req, users, otps, sessions, postgres.NewTxManager(pool), codes, usecase.SystemClock{},
				usecase.OTPVerifyConfig{MaxAttempts: 5, SessionTTL: 30 * 24 * time.Hour}),
			sessions: usecase.NewSessions(sessions, users, usecase.SystemClock{}),
		}
	}
	ts, err := turnstile.New("secret", cfURL, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	as.stack = newStack(t, stackOpts{otp: parts, channel: as.channel, turnstile: ts, otpPerMin: otpPerMin, admin: testAdmin(t), adminPerMin: 1_000_000})
	return as
}

func (s *stack) postJSON(t *testing.T, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := s.client.Post(s.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

func errField(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		c, _ := e["code"].(string)
		return c
	}
	return ""
}

const regBody = `{"name":"Budi","email":"budi@example.com","phone":"0812-3456-7890","turnstile_token":"human"}`

func TestE2EAuthRoutesAbsentInModeNone(t *testing.T) {
	s := newStack(t, stackOpts{})
	for _, p := range []string{"/api/v1/auth/register", "/api/v1/auth/otp/request"} {
		if resp, _ := s.postJSON(t, p, regBody); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status %d, ingin 404", p, resp.StatusCode)
		}
	}
}

func TestE2ERegisterAndRequest(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)

	resp, m := s.postJSON(t, "/api/v1/auth/register", regBody)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d: %v", resp.StatusCode, m)
	}
	id, err := uuid.Parse(m["challenge_id"].(string))
	if err != nil || m["status"] != "queued" || m["resend_after_seconds"] != 60.0 || m["estimated_wait_seconds"] != 2.0 {
		t.Fatalf("respons = %v", m)
	}
	if len(m) != 4 {
		t.Fatalf("respons tidak boleh memuat field lain: %v", m)
	}
	var status string
	if err := s.pool.QueryRow(context.Background(), `SELECT status FROM otp_challenges WHERE id = $1`, id).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("challenge di DB: %q %v", status, err)
	}

	// permintaan kedua untuk nomor yang sama dalam cooldown → 429 + Retry-After
	resp, m = s.postJSON(t, "/api/v1/auth/otp/request", `{"phone":"+6281234567890","turnstile_token":"human"}`)
	if resp.StatusCode != http.StatusTooManyRequests || errField(m) != "RATE_LIMITED" || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("status %d %v retry-after=%q", resp.StatusCode, m, resp.Header.Get("Retry-After"))
	}

	// nomor tak terdaftar: 202 berbentuk sama, tanpa challenge
	resp, m = s.postJSON(t, "/api/v1/auth/otp/request", `{"phone":"081299998888","turnstile_token":"human"}`)
	if resp.StatusCode != http.StatusAccepted || len(m) != 4 || m["status"] != "queued" {
		t.Fatalf("status %d %v", resp.StatusCode, m)
	}
	var n int
	_ = s.pool.QueryRow(context.Background(), `SELECT count(*) FROM otp_challenges`).Scan(&n)
	if n != 1 {
		t.Fatalf("challenge = %d, ingin 1", n)
	}
}

func TestE2EAuthErrors(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	cases := []struct {
		name, path, body string
		status           int
		code             string
	}{
		{"turnstile salah", "/api/v1/auth/register", strings.Replace(regBody, `"human"`, `"bot"`, 1), 403, "TURNSTILE_FAILED"},
		{"turnstile kosong", "/api/v1/auth/otp/request", `{"phone":"081234567890"}`, 403, "TURNSTILE_FAILED"},
		{"field tak dikenal", "/api/v1/auth/register", `{"name":"a","email":"a@b.co","phone":"0812","turnstile_token":"human","role":"admin"}`, 400, "INVALID_JSON"},
		{"nomor JID grup", "/api/v1/auth/otp/request", `{"phone":"120363025@g.us","turnstile_token":"human"}`, 422, "INVALID_PHONE"},
		{"email salah", "/api/v1/auth/register", strings.Replace(regBody, "budi@example.com", "budi", 1), 422, "INVALID_EMAIL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, m := s.postJSON(t, tc.path, tc.body)
			if resp.StatusCode != tc.status || errField(m) != tc.code {
				t.Fatalf("status %d code %q, ingin %d %s", resp.StatusCode, errField(m), tc.status, tc.code)
			}
		})
	}
	// kanal WhatsApp bermasalah → 503 OTP_UNAVAILABLE
	for i := 0; i < 3; i++ {
		s.channel.RecordSend(false)
	}
	if resp, m := s.postJSON(t, "/api/v1/auth/register", regBody); resp.StatusCode != 503 || errField(m) != "OTP_UNAVAILABLE" {
		t.Fatalf("status %d %v", resp.StatusCode, m)
	}
}

func TestE2ETurnstileDownFailsClosed(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)
	cf.Close()
	if resp, m := s.postJSON(t, "/api/v1/auth/register", regBody); resp.StatusCode != 503 || errField(m) != "TURNSTILE_UNAVAILABLE" {
		t.Fatalf("status %d %v", resp.StatusCode, m)
	}
	var n int
	_ = s.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n)
	if n != 0 {
		t.Fatal("tanpa verifikasi manusia tidak boleh ada pengguna dibuat")
	}
}

func TestE2EOTPRoutesShareIPLimit(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 2)
	s.postJSON(t, "/api/v1/auth/register", regBody)
	s.postJSON(t, "/api/v1/auth/otp/request", `{"phone":"081211112222","turnstile_token":"human"}`)
	resp, m := s.postJSON(t, "/api/v1/auth/otp/request", `{"phone":"081233334444","turnstile_token":"human"}`)
	if resp.StatusCode != http.StatusTooManyRequests || errField(m) != "RATE_LIMITED" {
		t.Fatalf("register + otp/request harus berbagi batas per IP: %d %v", resp.StatusCode, m)
	}
}

func (s *stack) getJSON(t *testing.T, path string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := s.client.Get(s.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

func TestE2EOTPStatusAndChannel(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)

	resp, m := s.getJSON(t, "/api/v1/auth/channel-status")
	if resp.StatusCode != 200 || m["otp_available"] != true || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("channel-status = %d %v", resp.StatusCode, m)
	}

	_, reg := s.postJSON(t, "/api/v1/auth/register", regBody)
	resp, m = s.getJSON(t, "/api/v1/auth/otp/"+reg["challenge_id"].(string))
	if resp.StatusCode != 200 || m["status"] != "queued" || m["failure_reason"] != nil || m["expires_at"] != nil ||
		m["resend_after_seconds"] != 60.0 || len(m) != 4 {
		t.Fatalf("status = %d %v", resp.StatusCode, m)
	}
	_, _ = s.pool.Exec(context.Background(), `UPDATE otp_challenges SET status = 'failed', failure_reason = 'WA_UNAVAILABLE'`)
	_, m = s.getJSON(t, "/api/v1/auth/otp/"+reg["challenge_id"].(string))
	if m["status"] != "failed" || m["failure_reason"] != "WA_UNAVAILABLE" || m["resend_after_seconds"] != 0.0 {
		t.Fatalf("status gagal = %v", m)
	}
	// id tak dikenal atau bukan UUID: queued, tanpa membuka informasi
	for _, id := range []string{uuid.NewString(), "bukan-uuid"} {
		resp, m = s.getJSON(t, "/api/v1/auth/otp/"+id)
		if resp.StatusCode != 200 || m["status"] != "queued" {
			t.Fatalf("%s: %d %v", id, resp.StatusCode, m)
		}
	}

	for i := 0; i < 3; i++ {
		s.channel.RecordSend(false)
	}
	if _, m = s.getJSON(t, "/api/v1/auth/channel-status"); m["otp_available"] != false {
		t.Fatalf("channel-status setelah gagal = %v", m)
	}
}

func TestE2EOTPStatusRoutesAbsentInModeNone(t *testing.T) {
	s := newStack(t, stackOpts{})
	for _, p := range []string{"/api/v1/auth/channel-status", "/api/v1/auth/otp/" + uuid.NewString()} {
		if resp, _ := s.getJSON(t, p); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
	}
}

func (s *stack) authed(t *testing.T, method, path, token string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, s.srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

func TestE2ELoginFlow(t *testing.T) {
	cf := fakeSiteverify(t)
	s := newAuthStack(t, cf.URL, 0)

	_, reg := s.postJSON(t, "/api/v1/auth/register", regBody)
	id := reg["challenge_id"].(string)
	if res, err := s.disp.DispatchOnce(context.Background()); err != nil || res.Outcome != usecase.DispatchSent {
		t.Fatalf("dispatch: %+v %v", res, err)
	}
	code := s.wa.LastCodeFor("6281234567890")

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	resp, m := s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+id+`","code":"`+wrong+`"}`)
	det, _ := m["error"].(map[string]any)["details"].(map[string]any)
	if resp.StatusCode != 422 || errField(m) != "OTP_INVALID" || det["attempts_left"] != 4.0 {
		t.Fatalf("kode salah: %d %v", resp.StatusCode, m)
	}

	resp, m = s.postJSON(t, "/api/v1/auth/otp/verify", `{"challenge_id":"`+id+`","code":"`+code+`"}`)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("verify: %d %v", resp.StatusCode, m)
	}
	token := m["access_token"].(string)
	user := m["user"].(map[string]any)
	if user["phone"] != "62812****7890" || user["role"] != "user" || user["name"] != "Budi" || m["expires_at"] == nil {
		t.Fatalf("user = %v", m)
	}

	resp, m = s.authed(t, "GET", "/api/v1/auth/me", "Bearer "+token)
	if resp.StatusCode != 200 || m["phone"] != "62812****7890" || m["email"] != "budi@example.com" {
		t.Fatalf("me: %d %v", resp.StatusCode, m)
	}
	if resp, _ = s.authed(t, "GET", "/api/v1/auth/me", "bearer "+token); resp.StatusCode != 200 {
		t.Fatalf("skema Bearer tidak peka huruf besar: %d", resp.StatusCode)
	}
	for _, h := range []string{"", "Bearer", "Basic " + token, "Bearer " + token + "x", token} {
		resp, m = s.authed(t, "GET", "/api/v1/auth/me", h)
		if resp.StatusCode != 401 || errField(m) != "UNAUTHORIZED" || resp.Header.Get("WWW-Authenticate") == "" {
			t.Fatalf("header %q: %d %v", h, resp.StatusCode, m)
		}
	}

	if resp, _ = s.authed(t, "POST", "/api/v1/auth/logout", "Bearer "+token); resp.StatusCode != 204 {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if resp, _ = s.authed(t, "GET", "/api/v1/auth/me", "Bearer "+token); resp.StatusCode != 401 {
		t.Fatalf("me setelah logout: %d", resp.StatusCode)
	}
	if resp, _ = s.authed(t, "POST", "/api/v1/auth/logout", "Bearer "+token); resp.StatusCode != 401 {
		t.Fatalf("logout ulang: %d", resp.StatusCode)
	}

	// token basi di header tidak menghalangi login ulang lewat rute publik
	req, _ := http.NewRequest("POST", s.srv.URL+"/api/v1/auth/otp/request", strings.NewReader(`{"phone":"081299990000","turnstile_token":"human"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r2, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusAccepted {
		t.Fatalf("otp/request dengan token basi: %d", r2.StatusCode)
	}
}

func TestE2ESessionRoutesAbsentInModeNone(t *testing.T) {
	s := newStack(t, stackOpts{})
	if resp, _ := s.authed(t, "GET", "/api/v1/auth/me", ""); resp.StatusCode != 404 {
		t.Fatalf("me: %d", resp.StatusCode)
	}
	if resp, _ := s.postJSON(t, "/api/v1/auth/otp/verify", `{}`); resp.StatusCode != 404 {
		t.Fatalf("verify: %d", resp.StatusCode)
	}
}
