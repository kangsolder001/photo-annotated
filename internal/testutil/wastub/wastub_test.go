package wastub_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"photo-annotated/internal/testutil/wastub"
)

func post(t *testing.T, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url+"/api/v1/messages/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func errCode(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		c, _ := e["code"].(string)
		return c
	}
	return ""
}

func TestStubRecordsMessagesAndExtractsCode(t *testing.T) {
	s := wastub.NewServer(t, wastub.Options{Token: "tok"})
	code, out := post(t, s.URL, "tok", `{"to":"6281234567890","message":"*482913* adalah kode verifikasi Anda. Berlaku 5 menit."}`)
	if code != 200 || out["success"] != true {
		t.Fatalf("%d %v", code, out)
	}
	data := out["data"].(map[string]any)
	if data["status"] != "SENT" || data["message_id"] == "" || data["to"] != "6281234567890@s.whatsapp.net" {
		t.Fatalf("data: %v", data)
	}
	if got := s.LastCodeFor("6281234567890"); got != "482913" {
		t.Fatalf("LastCodeFor = %q", got)
	}
	if got := s.LastCodeFor("628100000000"); got != "" {
		t.Fatalf("nomor lain: %q", got)
	}
	// pesan ke-1000+ tidak boleh membuat stub panik
	for i := 0; i < 1100; i++ {
		post(t, s.URL, "tok", `{"to":"6281234567890","message":"*111111* x"}`)
	}
	if n := len(s.Messages()); n != 1101 {
		t.Fatalf("pesan = %d", n)
	}
}

func TestCodeFrom(t *testing.T) {
	for in, want := range map[string]string{
		"*123456* adalah kode":        "123456",
		"Kode Anda *0042* ya":         "0042",
		"tanpa kode":                  "",
		"*12* terlalu pendek":         "",
		"*123456789* terlalu panjang": "",
		"kode 123456 tanpa bintang":   "",
	} {
		if got := wastub.CodeFrom(in); got != want {
			t.Errorf("CodeFrom(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStubAuthMatchesWaNotif(t *testing.T) {
	s := wastub.NewServer(t, wastub.Options{Token: "rahasia"})
	for name, tok := range map[string]string{"tanpa token": "", "token salah": "salah"} {
		code, out := post(t, s.URL, tok, `{"to":"6281234567890","message":"x"}`)
		if code != 401 || errCode(out) != "UNAUTHORIZED" {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if s.AuthFailures() != 2 || len(s.Messages()) != 0 {
		t.Fatalf("auth gagal=%d pesan=%d", s.AuthFailures(), len(s.Messages()))
	}
	// status PUBLIK (seperti wa-notif asli), tanpa token
	resp, err := http.Get(s.URL + "/api/v1/session/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status harus publik: %d", resp.StatusCode)
	}
	// tanpa token terkonfigurasi → terbuka
	open := wastub.NewServer(t, wastub.Options{})
	if code, _ := post(t, open.URL, "", `{"to":"6281234567890","message":"x"}`); code != 200 {
		t.Fatalf("tanpa token terkonfigurasi harus terbuka: %d", code)
	}
}

func TestStubFailureModes(t *testing.T) {
	s := wastub.NewServer(t, wastub.Options{})
	if code, out := post(t, s.URL, "", `{bukan json`); code != 400 || errCode(out) != "INVALID_REQUEST_BODY" {
		t.Fatalf("JSON rusak: %d %v", code, out)
	}
	s.FailNext(2)
	for i := 0; i < 2; i++ {
		if code, out := post(t, s.URL, "", `{"to":"6281234567890","message":"x"}`); code != 500 || errCode(out) != "SEND_FAILED" {
			t.Fatalf("FailNext %d: %d %v", i, code, out)
		}
	}
	if code, _ := post(t, s.URL, "", `{"to":"6281234567890","message":"x"}`); code != 200 {
		t.Fatalf("setelah FailNext habis harus normal: %d", code)
	}
	s.SetDisconnected(true)
	if code, out := post(t, s.URL, "", `{"to":"6281234567890","message":"x"}`); code != 503 || errCode(out) != "WA_NOT_CONNECTED" {
		t.Fatalf("terputus: %d %v", code, out)
	}
	resp, _ := http.Get(s.URL + "/api/v1/session/status")
	var st struct{ Data struct{ Connected bool } }
	_ = json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if st.Data.Connected {
		t.Fatal("status harus menunjukkan terputus")
	}
	s.SetDisconnected(false)
	s.SetBadBody(true)
	resp, _ = http.Post(s.URL+"/api/v1/messages/send", "application/json", strings.NewReader(`{"to":"6281234567890","message":"x"}`))
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "captive portal") {
		t.Fatalf("bad body: %d %s", resp.StatusCode, b)
	}
}

func TestStubSerializesSendsWithInterval(t *testing.T) {
	const gap = 60 * time.Millisecond
	s := wastub.NewServer(t, wastub.Options{SendInterval: gap})
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			post(t, s.URL, "", `{"to":"6281234567890","message":"x"}`)
		}()
	}
	wg.Wait()
	if el := time.Since(start); el < 3*gap-10*time.Millisecond {
		t.Fatalf("4 pengiriman serentak harus diserialkan (≥ %v), selesai dalam %v", 3*gap, el)
	}
}

func TestStubKeepsRawDestination(t *testing.T) {
	// wa-notif menerima JID mentah; stub mencatat apa adanya agar test bisa membuktikan bahwa API kita tidak pernah mengirimnya
	s := wastub.NewServer(t, wastub.Options{})
	post(t, s.URL, "", `{"to":"6281234567890-1600000000@g.us","message":"x"}`)
	if m := s.Messages(); len(m) != 1 || m[0].To != "6281234567890-1600000000@g.us" {
		t.Fatalf("%+v", m)
	}
}
