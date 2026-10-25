package wanotif_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"photo-annotated/internal/adapter/wanotif"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/wastub"
	"photo-annotated/internal/usecase"
)

const (
	phone  = "6281234567890"
	secret = "TOKEN-RAHASIA-WA"
	otpMsg = "*482913* adalah kode verifikasi Photo Annotated Anda. Berlaku 5 menit."
)

func newClient(t *testing.T, url string, timeout time.Duration) *wanotif.Client {
	t.Helper()
	c, err := wanotif.New(url, secret, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sendErr(t *testing.T, err error) *usecase.SendError {
	t.Helper()
	var se *usecase.SendError
	if !errors.As(err, &se) {
		t.Fatalf("harus *usecase.SendError, dapat %T: %v", err, err)
	}
	return se
}

// ---------------------------------------------------------------- New

func TestNewValidates(t *testing.T) {
	for _, u := range []string{"", "bukan url", "ftp://x:3030", "http://", "wa-notif:3030"} {
		if _, err := wanotif.New(u, "t", time.Second); err == nil {
			t.Errorf("base_url %q harus ditolak", u)
		}
	}
	if _, err := wanotif.New("http://wa-notif-service:3030", "t", 0); err == nil {
		t.Error("timeout 0 harus ditolak")
	}
	if _, err := wanotif.New("http://wa-notif-service:3030/", "t", time.Second); err != nil {
		t.Errorf("garis miring di ujung harus diterima: %v", err)
	}
}

// ---------------------------------------------------------------- Send: sukses dan bentuk request

func TestSendSuccessThroughStub(t *testing.T) {
	stub := wastub.NewServer(t, wastub.Options{Token: secret})
	c := newClient(t, stub.URL, 2*time.Second)
	id, err := c.Send(context.Background(), phone, otpMsg)
	if err != nil || !strings.HasPrefix(id, "STUB") {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if got := stub.LastCodeFor(phone); got != "482913" {
		t.Fatalf("kode yang diterima stub = %q", got)
	}
	if m := stub.Messages(); len(m) != 1 || m[0].To != phone || m[0].Text != otpMsg {
		t.Fatalf("pesan: %+v", m)
	}
}

func TestSendRequestShape(t *testing.T) {
	var gotAuth, gotCT, gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotCT, gotPath, gotMethod = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"success":true,"data":{"message_id":"ABC123"}}`))
	}))
	defer srv.Close()
	c := newClient(t, srv.URL+"/", time.Second)
	id, err := c.Send(context.Background(), phone, otpMsg)
	if err != nil || id != "ABC123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if gotMethod != "POST" || gotPath != "/api/v1/messages/send" || gotAuth != "Bearer "+secret || gotCT != "application/json" {
		t.Fatalf("method=%s path=%s auth=%q ct=%q", gotMethod, gotPath, gotAuth, gotCT)
	}
	if len(gotBody) != 2 || gotBody["to"] != phone || gotBody["message"] != otpMsg {
		t.Fatalf("body = %v (hanya to dan message)", gotBody)
	}
}

// ---------------------------------------------------------------- Send: pagar nomor kanonik

func TestSendRefusesNonCanonicalDestinationsWithoutTouchingTheNetwork(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	c := newClient(t, srv.URL, time.Second)
	for name, bad := range map[string]string{
		"JID grup":       "6281234567890-1600000000@g.us",
		"JID pengguna":   "6281234567890@s.whatsapp.net",
		"format lokal":   "081234567890",
		"dengan plus":    "+6281234567890",
		"kosong":         "",
		"huruf":          "62812abc7890",
		"spasi":          "62812 34567890",
		"newline":        "6281234567890\n",
		"negara lain":    "14155552671",
		"terlalu pendek": "62812",
	} {
		_, err := c.Send(context.Background(), bad, otpMsg)
		if err == nil {
			t.Errorf("%s (%q): seharusnya ditolak", name, bad)
			continue
		}
		se := sendErr(t, err)
		if se.Reason != domain.OTPFailSendFailed || se.Retryable || !se.Alert {
			t.Errorf("%s: %+v", name, se)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("server dipanggil %d kali; tujuan tak kanonik tidak boleh menyentuh jaringan", calls.Load())
	}
}

// ---------------------------------------------------------------- Send: pemetaan error

func TestSendErrorMapping(t *testing.T) {
	type tc struct {
		name      string
		status    int
		body      string
		reason    string
		retryable bool
		alert     bool
	}
	cases := []tc{
		{"503 tidak tersambung", 503, `{"success":false,"error":{"code":"WA_NOT_CONNECTED","message":"x"}}`, domain.OTPFailWAUnavailable, true, false},
		{"503 tanpa badan", 503, ``, domain.OTPFailWAUnavailable, true, false},
		{"500 SEND_FAILED", 500, `{"success":false,"error":{"code":"SEND_FAILED","message":"x"}}`, domain.OTPFailSendFailed, true, false},
		{"401 token ditolak", 401, `{"success":false,"error":{"code":"UNAUTHORIZED","message":"x"}}`, domain.OTPFailWAUnavailable, false, true},
		{"400 request ditolak", 400, `{"success":false,"error":{"code":"INVALID_REQUEST_BODY","message":"x"}}`, domain.OTPFailSendFailed, false, true},
		{"404", 404, `not found`, domain.OTPFailSendFailed, false, true},
		{"405", 405, ``, domain.OTPFailSendFailed, false, true},
		{"429", 429, ``, domain.OTPFailWAUnavailable, true, false},
		{"502 dari proxy", 502, `<html>bad gateway</html>`, domain.OTPFailWAUnavailable, true, false},
		{"504 dari proxy", 504, ``, domain.OTPFailWAUnavailable, true, false},
		{"200 bukan JSON", 200, `<html>captive portal</html>`, domain.OTPFailWAUnavailable, true, false},
		{"200 success=false", 200, `{"success":false}`, domain.OTPFailWAUnavailable, true, false},
		{"200 tanpa message_id", 200, `{"success":true,"data":{}}`, domain.OTPFailWAUnavailable, true, false},
		{"200 data bukan objek", 200, `{"success":true,"data":"ok"}`, domain.OTPFailWAUnavailable, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			_, err := newClient(t, srv.URL, time.Second).Send(context.Background(), phone, otpMsg)
			se := sendErr(t, err)
			if se.Reason != c.reason || se.Retryable != c.retryable || se.Alert != c.alert {
				t.Fatalf("got %+v, want reason=%s retryable=%v alert=%v", se, c.reason, c.retryable, c.alert)
			}
		})
	}
}

func TestSendNeverLeaksMessageOrTokenInErrors(t *testing.T) {
	for _, status := range []int{400, 401, 500, 503, 502} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			// server yang "memantulkan" isi permintaan dan token (kasus terburuk)
			b, _ := io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":"X","message":"` + string(b) + r.Header.Get("Authorization") + `"}}`))
		}))
		_, err := newClient(t, srv.URL, time.Second).Send(context.Background(), phone, otpMsg)
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: seharusnya error", status)
		}
		for _, secretText := range []string{secret, "482913", "Bearer", "kode verifikasi"} {
			if strings.Contains(err.Error(), secretText) {
				t.Fatalf("status %d: error membocorkan %q: %v", status, secretText, err)
			}
		}
	}
}

func TestSendNetworkFailures(t *testing.T) {
	// koneksi ditolak
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	_, err := newClient(t, "http://"+addr, time.Second).Send(context.Background(), phone, otpMsg)
	se := sendErr(t, err)
	if se.Reason != domain.OTPFailWAUnavailable || !se.Retryable || se.Alert {
		t.Fatalf("koneksi ditolak: %+v", se)
	}
	if strings.Contains(err.Error(), "/api/v1/messages/send") || strings.Contains(err.Error(), "Post ") {
		t.Fatalf("URL lengkap permintaan tidak boleh ikut di pesan error: %v", err)
	}

	// timeout klien
	stub := wastub.NewServer(t, wastub.Options{})
	stub.SetHang(true)
	start := time.Now()
	_, err = newClient(t, stub.URL, 150*time.Millisecond).Send(context.Background(), phone, otpMsg)
	se = sendErr(t, err)
	if se.Reason != domain.OTPFailWAUnavailable || !se.Retryable {
		t.Fatalf("timeout: %+v", se)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("timeout tidak bekerja: %v", el)
	}
}

func TestSendPropagatesContextCancellationUnchanged(t *testing.T) {
	stub := wastub.NewServer(t, wastub.Options{})
	stub.SetHang(true)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := newClient(t, stub.URL, 5*time.Second).Send(ctx, phone, otpMsg)
	var se *usecase.SendError
	if errors.As(err, &se) || !errors.Is(err, context.Canceled) {
		t.Fatalf("pembatalan (shutdown) bukan kegagalan gateway dan harus context.Canceled: %v", err)
	}
}

func TestSendDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
	}))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redir.Close()
	_, err := newClient(t, redir.URL, time.Second).Send(context.Background(), phone, otpMsg)
	if err == nil || leaked.Load() {
		t.Fatalf("redirect tidak boleh diikuti (token bisa bocor): err=%v leaked=%v", err, leaked.Load())
	}
}

func TestSendLimitsResponseSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"success":true,"data":{"message_id":"` + strings.Repeat("A", 1<<20) + `"}}`))
	}))
	defer srv.Close()
	_, err := newClient(t, srv.URL, 2*time.Second).Send(context.Background(), phone, otpMsg)
	if err == nil {
		t.Fatal("badan > 64 KB dipotong sehingga JSON tidak utuh dan harus gagal, bukan menelan memori tanpa batas")
	}
}

// ---------------------------------------------------------------- Check (probe)

func TestCheckConnectedAndDisconnected(t *testing.T) {
	stub := wastub.NewServer(t, wastub.Options{Token: secret})
	c := newClient(t, stub.URL, time.Second)
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("tersambung: %v", err)
	}
	stub.SetDisconnected(true)
	err := c.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("terputus: %v", err)
	}
	stub.SetDisconnected(false)
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("pulih: %v", err)
	}
}

func TestCheckDoesNotNeedOrSendTheToken(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"CONNECTED","connected":true}}`))
	}))
	defer srv.Close()
	if err := newClient(t, srv.URL, time.Second).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Fatalf("endpoint status publik: token tidak perlu dan tidak boleh dikirim: %q", auth)
	}
}

func TestCheckFailures(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"HTTP 500":      func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"HTTP 404":      func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) },
		"bukan JSON":    func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"success=false": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"success":false}`)) },
		"connected=false": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"success":true,"data":{"status":"PAIRING","connected":false}}`))
		},
		"data kosong": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"success":true}`)) },
	} {
		srv := httptest.NewServer(h)
		err := newClient(t, srv.URL, time.Second).Check(context.Background())
		srv.Close()
		if err == nil {
			t.Errorf("%s: seharusnya gagal", name)
		}
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if err := newClient(t, "http://"+addr, time.Second).Check(context.Background()); err == nil {
		t.Error("tidak terjangkau: seharusnya gagal")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newClient(t, "http://127.0.0.1:1", time.Second).Check(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("konteks batal: %v", err)
	}
}
