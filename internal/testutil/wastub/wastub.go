// Package wastub meniru API wa-notif (POST /api/v1/messages/send, GET /api/v1/session/status) untuk
// test dan load test, termasuk perilaku pentingnya: pengiriman diserialkan dengan jeda antar pesan
// (rate_limit_seconds), status/QR publik tanpa token, pengiriman butuh Bearer token, dan semua error
// berformat {"success":false,"error":{"code":...}}. Setiap pesan yang diterima dicatat sehingga test
// dapat membaca kode OTP yang "terkirim". JANGAN pernah mengarahkan beban uji ke wa-notif asli.
package wastub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Options mengatur perilaku stub.
type Options struct {
	Token        string        // kosong = tanpa autentikasi (seperti wa-notif tanpa bearer_token)
	SendInterval time.Duration // jeda minimum antar pengiriman (diserialkan seperti wa-notif)
	Latency      time.Duration // penundaan tambahan tiap pengiriman
}

// Message adalah satu pesan yang diterima stub.
type Message struct {
	To   string // nilai mentah field "to" yang dikirim klien
	Text string
	At   time.Time
}

// State menampung data yang dapat diubah dan dibaca saat stub berjalan.
type State struct {
	opts Options

	mu           sync.Mutex
	messages     []Message
	disconnected bool
	failNext     int  // jumlah pengiriman berikutnya yang dijawab 500 SEND_FAILED
	hang         bool // pengiriman menggantung sampai klien menyerah
	badBody      bool // pengiriman dijawab 200 dengan badan yang bukan JSON
	lastSend     time.Time
	sendMu       sync.Mutex // serialisasi pengiriman seperti applyRateLimit di wa-notif

	sendCalls   atomic.Int64
	statusCalls atomic.Int64
	authFails   atomic.Int64
}

func (s *State) SetDisconnected(v bool) { s.mu.Lock(); s.disconnected = v; s.mu.Unlock() }
func (s *State) FailNext(n int)         { s.mu.Lock(); s.failNext = n; s.mu.Unlock() }
func (s *State) SetHang(v bool)         { s.mu.Lock(); s.hang = v; s.mu.Unlock() }
func (s *State) SetBadBody(v bool)      { s.mu.Lock(); s.badBody = v; s.mu.Unlock() }

// Messages mengembalikan salinan pesan yang diterima, berurutan.
func (s *State) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.messages...)
}

func (s *State) SendCalls() int64    { return s.sendCalls.Load() }
func (s *State) StatusCalls() int64  { return s.statusCalls.Load() }
func (s *State) AuthFailures() int64 { return s.authFails.Load() }

var codeRe = regexp.MustCompile(`\*(\d{4,8})\*`)

// CodeFrom mengambil kode OTP dari teks pesan berbentuk "*123456* adalah …".
func CodeFrom(text string) string {
	if m := codeRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// LastCodeFor mengembalikan kode OTP terakhir yang dikirim ke nomor itu ("" bila belum ada).
func (s *State) LastCodeFor(phone string) string {
	msgs := s.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].To == phone {
			return CodeFrom(msgs[i].Text)
		}
	}
	return ""
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiResponse struct {
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *apiError `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiResponse{Error: &apiError{Code: code, Message: msg}})
}

// NewHandler membuat state dan http.Handler yang meniru wa-notif, ditambah /_stub/* untuk inspeksi.
func NewHandler(opts Options) (*State, http.Handler) {
	s := &State{opts: opts}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "UP"})
	})
	// publik, seperti di wa-notif asli
	mux.HandleFunc("GET /api/v1/session/status", func(w http.ResponseWriter, r *http.Request) {
		s.statusCalls.Add(1)
		s.mu.Lock()
		dis := s.disconnected
		s.mu.Unlock()
		status := "CONNECTED"
		if dis {
			status = "DISCONNECTED"
		}
		writeJSON(w, 200, apiResponse{Success: true, Data: map[string]any{"status": status, "connected": !dis}})
	})
	mux.HandleFunc("POST /api/v1/messages/send", s.send)

	mux.HandleFunc("GET /_stub/messages", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Messages()) })
	mux.HandleFunc("GET /_stub/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]int64{"send_calls": s.SendCalls(), "status_calls": s.StatusCalls(), "auth_failures": s.AuthFailures(), "messages": int64(len(s.Messages()))})
	})
	return s, mux
}

func (s *State) send(w http.ResponseWriter, r *http.Request) {
	s.sendCalls.Add(1)
	if s.opts.Token != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) != s.opts.Token {
			s.authFails.Add(1)
			fail(w, 401, "UNAUTHORIZED", "Missing or invalid Bearer token in Authorization header")
			return
		}
	}
	var req struct {
		To      string `json:"to"`
		Message string `json:"message"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := json.Unmarshal(body, &req); err != nil {
		fail(w, 400, "INVALID_REQUEST_BODY", "Malformed JSON payload")
		return
	}

	s.mu.Lock()
	dis, hang, bad := s.disconnected, s.hang, s.badBody
	failing := s.failNext > 0
	if failing {
		s.failNext--
	}
	s.mu.Unlock()

	if dis {
		fail(w, 503, "WA_NOT_CONNECTED", "whatsapp service is not connected. please scan the qr code first")
		return
	}
	if hang {
		<-r.Context().Done()
		return
	}
	// pengiriman diserialkan dengan jeda minimum, seperti applyRateLimit di wa-notif
	s.sendMu.Lock()
	if wait := s.opts.SendInterval - time.Since(s.lastSend); !s.lastSend.IsZero() && wait > 0 {
		time.Sleep(wait)
	}
	s.lastSend = time.Now()
	s.sendMu.Unlock()
	if s.opts.Latency > 0 {
		time.Sleep(s.opts.Latency)
	}

	if failing {
		fail(w, 500, "SEND_FAILED", "failed to send message via whatsapp")
		return
	}
	if bad {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("<html>captive portal</html>"))
		return
	}
	s.mu.Lock()
	s.messages = append(s.messages, Message{To: req.To, Text: req.Message, At: time.Now()})
	n := len(s.messages)
	s.mu.Unlock()
	writeJSON(w, 200, apiResponse{Success: true, Data: map[string]any{
		"message_id": fmt.Sprintf("STUB%08d", n),
		"to":         req.To + "@s.whatsapp.net", "status": "SENT", "timestamp": time.Now().UTC(),
	}})
}

// Server adalah stub yang berjalan di httptest.Server.
type Server struct {
	*State
	*httptest.Server
}

// NewServer menjalankan stub untuk sebuah test dan mematikannya saat test selesai.
func NewServer(t testing.TB, opts Options) *Server {
	t.Helper()
	st, h := NewHandler(opts)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Server{State: st, Server: srv}
}
