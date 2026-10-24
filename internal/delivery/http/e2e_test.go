package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/imagecheck"
	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	httpapi "photo-annotated/internal/delivery/http"
	"photo-annotated/internal/delivery/http/handler"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/imgfix"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

const (
	e2eSecret     = "0123456789abcdef0123456789abcdef0123456789abcdef"
	e2eEditSecret = "fedcba9876543210fedcba9876543210fedcba9876543210"
)

type stackOpts struct {
	minFree   int64
	viewTTL   time.Duration
	uploadTTL time.Duration
	maxConc   int
	maxBytes  int64
	// Auth OTP opsional (nil = mode none: rute /auth/* 404).
	otp       func(*pgxpool.Pool) otpParts
	channel   handler.ChannelStatus
	turnstile usecase.TurnstileVerifier
	otpPerMin int
}

// otpParts adalah usecase auth OTP yang dirakit test di atas pool milik stack.
type otpParts struct {
	req      handler.OTPRequester
	verify   handler.OTPVerifier
	sessions httpapi.SessionManager
}

type stack struct {
	pool   *pgxpool.Pool
	srv    *httptest.Server
	subs   *postgres.SubmissionRepo
	repo   *postgres.PhotoRepo
	signer *signer.Signer
	dir    string
	client *http.Client
}

// newStack merakit seluruh backend dengan Postgres, localfs, signer, dan imagecheck sungguhan
// di belakang server HTTP asli (Content-Length, chunked, dan deadline bekerja seperti produksi).
func newStack(t *testing.T, o stackOpts) *stack {
	t.Helper()
	if o.uploadTTL == 0 {
		o.uploadTTL = 10 * time.Minute
	}
	if o.viewTTL == 0 {
		o.viewTTL = 5 * time.Minute
	}
	if o.maxConc == 0 {
		o.maxConc = 200
	}
	if o.maxBytes == 0 {
		o.maxBytes = 2 << 20
	}
	if o.otpPerMin == 0 {
		o.otpPerMin = 1_000_000
	}
	pool := pgtest.Pool(t)
	repo := postgres.NewPhotoRepo(pool)
	dir := t.TempDir()
	store, err := localfs.New(localfs.Options{Root: dir, MinFreeBytes: o.minFree})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sg, err := signer.New(e2eSecret)
	if err != nil {
		t.Fatal(err)
	}

	txm := postgres.NewTxManager(pool)
	subs := postgres.NewSubmissionRepo(pool)
	et, err := signer.NewEditTokens(e2eEditSecret)
	if err != nil {
		t.Fatal(err)
	}
	submit := usecase.NewSubmit(repo, subs, txm, store, et, usecase.SystemClock{},
		usecase.SubmitConfig{MaxPhotos: 5, MaxAnnotationsPerPhoto: 50, MaxAnnotationTextLen: 1000})

	var inner atomic.Pointer[http.Handler]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(*inner.Load()).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	reader := usecase.NewReader(subs, repo, store, sg, et, usecase.SystemClock{}, usecase.ReadConfig{
		PublicBaseURL: srv.URL, EditWindow: 24 * time.Hour, ViewURLTTL: o.viewTTL,
	})
	editor := usecase.NewEdit(repo, subs, txm, store, et, usecase.SystemClock{}, usecase.EditConfig{
		SubmitConfig: usecase.SubmitConfig{MaxPhotos: 5, MaxAnnotationsPerPhoto: 50, MaxAnnotationTextLen: 1000},
		Window:       24 * time.Hour,
	})
	presign := usecase.NewPresign(repo, store, sg, usecase.SystemClock{}, usecase.PresignConfig{
		PublicBaseURL: srv.URL, MaxUploadBytes: o.maxBytes, UploadURLTTL: o.uploadTTL,
	})
	upload := usecase.NewUpload(repo, store, sg, imagecheck.Inspector{}, usecase.SystemClock{},
		usecase.UploadConfig{MaxConcurrent: o.maxConc})
	var otp otpParts
	if o.otp != nil {
		otp = o.otp(pool)
	}
	var h http.Handler = httpapi.NewRouter(httpapi.Deps{
		Logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		TrustedProxies: []netip.Prefix{},
		DB:             pool,
		AllowedOrigins: []string{"https://app.example.com"},
		Presigner:      presign, PresignPerMin: 1_000_000,
		Uploader: upload, MaxUploadBytes: o.maxBytes, UploadTimeout: time.Minute,
		Submitter: submit, SubmissionsPerMin: 1_000_000,
		Reader: reader, Editor: editor,
		OTPRequester: otp.req, OTPVerifier: otp.verify, Sessions: otp.sessions, OTPVerifyPerMin: 1_000_000, OTPChannel: o.channel, Turnstile: o.turnstile, OTPRequestPerMin: o.otpPerMin,
	})
	inner.Store(&h)
	return &stack{pool: pool, srv: srv, repo: repo, subs: subs, signer: sg, dir: dir, client: &http.Client{Timeout: 60 * time.Second}}
}

type presigned struct {
	ID  uuid.UUID
	URL string
}

func (s *stack) presign(t testing.TB, ct string, size, w, h int) presigned {
	t.Helper()
	body := `{"content_type":"` + ct + `","size_bytes":` + itoa(size) + `,"width":` + itoa(w) + `,"height":` + itoa(h) + `}`
	resp, err := s.client.Post(s.srv.URL+"/api/v1/uploads/presign", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		t.Fatalf("presign status %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		PhotoID uuid.UUID `json:"photo_id"`
		Upload  struct {
			URL string `json:"url"`
		} `json:"upload"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return presigned{ID: out.PhotoID, URL: out.Upload.URL}
}

func itoa(n int) string { return strconv.Itoa(n) }

type putResult struct {
	Status int
	Code   string // kode error JSON, bila ada
	Header http.Header
}

func (s *stack) put(t testing.TB, url, ct string, body io.Reader, contentLength int64) putResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, body)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = contentLength // -1 → chunked
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	r := putResult{Status: resp.StatusCode, Header: resp.Header}
	var e struct{ Error struct{ Code string } }
	if json.Unmarshal(raw, &e) == nil {
		r.Code = e.Error.Code
	}
	return r
}

func (s *stack) putBytes(t testing.TB, url, ct string, body []byte) putResult {
	return s.put(t, url, ct, bytes.NewReader(body), int64(len(body)))
}

func (s *stack) photo(t testing.TB, id uuid.UUID) *domain.Photo {
	t.Helper()
	p, err := s.repo.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (s *stack) fileExists(p *domain.Photo) bool {
	_, err := os.Stat(filepath.Join(s.dir, p.StorageKey))
	return err == nil
}

func (s *stack) tmpEntries(t testing.TB) int {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(s.dir, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

func tamperSig(url string) string {
	i := strings.Index(url, "sig=") + 4
	c := byte('A')
	if url[i] == 'A' {
		c = 'B'
	}
	return url[:i] + string(c) + url[i+1:]
}

// ---------------------------------------------------------------- presign

func TestE2EPresignCreatesPendingPhoto(t *testing.T) {
	st := newStack(t, stackOpts{})
	pr := st.presign(t, "image/webp", 412345, 1920, 1440)
	p := st.photo(t, pr.ID)
	if p.Status != domain.PhotoPending || p.SizeBytes != 412345 || p.Width != 1920 || p.UploadedAt != nil ||
		!domain.IsValidStorageKey(p.StorageKey) {
		t.Fatalf("foto di DB salah: %+v", p)
	}
	if !strings.HasPrefix(pr.URL, st.srv.URL+"/api/v1/uploads/"+pr.ID.String()+"?exp=") {
		t.Fatalf("URL salah: %s", pr.URL)
	}
}

func TestE2EPresignRefusedWhenStorageFull(t *testing.T) {
	st := newStack(t, stackOpts{minFree: 1 << 62})
	resp, err := st.client.Post(st.srv.URL+"/api/v1/uploads/presign", "application/json",
		strings.NewReader(`{"content_type":"image/webp","size_bytes":1000,"width":10,"height":10}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 503 || !strings.Contains(string(raw), "STORAGE_FULL") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------- alur lengkap presign → PUT

func TestE2EFullUploadFlow(t *testing.T) {
	st := newStack(t, stackOpts{})
	body := imgfix.WebP(1920, 1440, 400*1024, 0x33)
	pr := st.presign(t, "image/webp", len(body), 1920, 1440)

	if r := st.putBytes(t, pr.URL, "image/webp", body); r.Status != 204 {
		t.Fatalf("PUT status %d (%s)", r.Status, r.Code)
	}
	p := st.photo(t, pr.ID)
	sum := sha256.Sum256(body)
	if p.UploadedAt == nil || !bytes.Equal(p.SHA256, sum[:]) || p.Status != domain.PhotoPending {
		t.Fatalf("catatan DB salah: %+v", p)
	}
	got, err := os.ReadFile(filepath.Join(st.dir, p.StorageKey))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("file di SSD salah: %v", err)
	}
	if st.tmpEntries(t) != 0 {
		t.Fatal("tmp harus bersih")
	}

	// retry (PUT ulang) pada foto pending: aman, menimpa
	body2 := imgfix.WebP(1920, 1440, len(body), 0x44)
	if r := st.putBytes(t, pr.URL, "image/webp", body2); r.Status != 204 {
		t.Fatalf("PUT ulang status %d (%s)", r.Status, r.Code)
	}
	got, _ = os.ReadFile(filepath.Join(st.dir, p.StorageKey))
	if !bytes.Equal(got, body2) {
		t.Fatal("PUT ulang harus menimpa file")
	}
}

func TestE2EJPEGUpload(t *testing.T) {
	st := newStack(t, stackOpts{})
	body := imgfix.JPEG(640, 480)
	pr := st.presign(t, "image/jpeg", len(body), 640, 480)
	if r := st.putBytes(t, pr.URL, "image/jpeg", body); r.Status != 204 {
		t.Fatalf("status %d (%s)", r.Status, r.Code)
	}
}

func TestE2EUploadFailures(t *testing.T) {
	st := newStack(t, stackOpts{maxBytes: 1 << 20})
	body := imgfix.WebP(100, 80, 5000, 0x01)

	t.Run("Content-Type header salah", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 80)
		if r := st.putBytes(t, pr.URL, "image/jpeg", body); r.Status != 422 || r.Code != "INVALID_CONTENT_TYPE" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("ukuran kurang dari deklarasi", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 80)
		if r := st.putBytes(t, pr.URL, "image/webp", body[:len(body)-2]); r.Status != 422 || r.Code != "SIZE_MISMATCH" {
			t.Fatalf("%+v", r)
		}
		p := st.photo(t, pr.ID)
		if p.Status != domain.PhotoPending || st.fileExists(p) || st.tmpEntries(t) != 0 {
			t.Fatalf("foto harus tetap pending dan bersih: %+v", p)
		}
		if r := st.putBytes(t, pr.URL, "image/webp", body); r.Status != 204 { // bisa diulang
			t.Fatalf("ulang: %+v", r)
		}
	})
	t.Run("ukuran lebih dari deklarasi", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 80)
		if r := st.putBytes(t, pr.URL, "image/webp", append(append([]byte{}, body...), 0, 0)); r.Status != 422 || r.Code != "SIZE_MISMATCH" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("tanpa Content-Length (chunked)", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 80)
		r := st.put(t, pr.URL, "image/webp", io.MultiReader(bytes.NewReader(body)), -1)
		if r.Status != 411 || r.Code != "LENGTH_REQUIRED" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("bukan gambar", func(t *testing.T) {
		junk := imgfix.Junk(len(body))
		pr := st.presign(t, "image/webp", len(junk), 100, 80)
		if r := st.putBytes(t, pr.URL, "image/webp", junk); r.Status != 422 || r.Code != "INVALID_IMAGE" {
			t.Fatalf("%+v", r)
		}
		p := st.photo(t, pr.ID)
		if p.Status != domain.PhotoRejected || st.fileExists(p) || st.tmpEntries(t) != 0 {
			t.Fatalf("foto harus rejected, tanpa file: %+v", p)
		}
		if r := st.putBytes(t, pr.URL, "image/webp", body); r.Status != 409 || r.Code != "PHOTO_NOT_AVAILABLE" {
			t.Fatalf("setelah rejected: %+v", r)
		}
	})
	t.Run("dimensi tidak sama dengan deklarasi", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 81)
		if r := st.putBytes(t, pr.URL, "image/webp", body); r.Status != 422 || r.Code != "INVALID_IMAGE" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("tanda tangan diubah", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 80)
		if r := st.putBytes(t, tamperSig(pr.URL), "image/webp", body); r.Status != 403 || r.Code != "INVALID_SIGNATURE" {
			t.Fatalf("%+v", r)
		}
		if p := st.photo(t, pr.ID); st.fileExists(p) {
			t.Fatal("tidak boleh ada file")
		}
	})
	t.Run("id tak dikenal", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 80)
		url := strings.Replace(pr.URL, pr.ID.String(), uuid.NewString(), 1)
		if r := st.putBytes(t, url, "image/webp", body); r.Status != 403 || r.Code != "INVALID_SIGNATURE" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("body melebihi batas global ditolak sebelum dibaca", func(t *testing.T) {
		pr := st.presign(t, "image/webp", len(body), 100, 80)
		huge := make([]byte, 2<<20) // > maxBytes 1 MB
		if r := st.putBytes(t, pr.URL, "image/webp", huge); r.Status != 413 || r.Code != "PAYLOAD_TOO_LARGE" {
			t.Fatalf("%+v", r)
		}
	})
}

func TestE2EExpiredURL(t *testing.T) {
	st := newStack(t, stackOpts{uploadTTL: time.Second})
	body := imgfix.WebP(100, 80, 2000, 0)
	pr := st.presign(t, "image/webp", len(body), 100, 80)
	time.Sleep(2200 * time.Millisecond)
	if r := st.putBytes(t, pr.URL, "image/webp", body); r.Status != 410 || r.Code != "URL_EXPIRED" {
		t.Fatalf("%+v", r)
	}
}

func TestE2EPreflightAndCORSOnPut(t *testing.T) {
	st := newStack(t, stackOpts{})
	req, _ := http.NewRequest("OPTIONS", st.srv.URL+"/api/v1/uploads/"+uuid.NewString(), nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	resp, err := st.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
	// respons PUT sungguhan membawa header CORS + mengekspos Retry-After untuk backoff FE
	body := imgfix.WebP(100, 80, 2000, 0)
	pr := st.presign(t, "image/webp", len(body), 100, 80)
	preq, _ := http.NewRequest("PUT", pr.URL, bytes.NewReader(body))
	preq.Header.Set("Content-Type", "image/webp")
	preq.Header.Set("Origin", "https://app.example.com")
	presp, err := st.client.Do(preq)
	if err != nil {
		t.Fatal(err)
	}
	presp.Body.Close()
	if presp.StatusCode != 204 || presp.Header.Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
		!strings.Contains(presp.Header.Get("Access-Control-Expose-Headers"), "Retry-After") {
		t.Fatalf("PUT lintas-origin: %d %v", presp.StatusCode, presp.Header)
	}
}

// ---------------------------------------------------------------- konkurensi

// runConcurrentUploads mengunggah n foto serentak lewat HTTP asli dan mengembalikan
// puncak HeapInuse yang teramati selama fase PUT dibanding sebelum fase itu.
func runConcurrentUploads(t *testing.T, st *stack, n, size int) (peakGrowth uint64) {
	t.Helper()
	body := imgfix.WebP(1920, 1440, size, 0x6B) // satu buffer dipakai bersama semua klien
	pres := make([]presigned, n)
	for i := range pres {
		pres[i] = st.presign(t, "image/webp", len(body), 1920, 1440)
	}

	runtimeGC()
	base := heapInuse()
	var peak atomic.Uint64
	stop := make(chan struct{})
	var sw sync.WaitGroup
	sw.Add(1)
	go func() {
		defer sw.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if h := heapInuse(); h > peak.Load() {
				peak.Store(h)
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()

	var wg sync.WaitGroup
	var failed atomic.Int32
	for i := range pres {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := st.putBytes(t, pres[i].URL, "image/webp", body); r.Status != 204 {
				failed.Add(1)
				t.Errorf("upload %d: %d %s", i, r.Status, r.Code)
			}
		}()
	}
	wg.Wait()
	close(stop)
	sw.Wait()
	if failed.Load() != 0 {
		t.FailNow()
	}

	sum := sha256.Sum256(body)
	for i := range pres {
		p := st.photo(t, pres[i].ID)
		if p.UploadedAt == nil || !bytes.Equal(p.SHA256, sum[:]) || !st.fileExists(p) {
			t.Fatalf("foto %d tidak lengkap: %+v", i, p)
		}
	}
	if st.tmpEntries(t) != 0 {
		t.Fatal("tmp harus bersih")
	}
	if peak.Load() > base {
		return peak.Load() - base
	}
	return 0
}

func TestE2EConcurrentUploadsAllSucceed(t *testing.T) {
	st := newStack(t, stackOpts{maxConc: 100})
	runConcurrentUploads(t, st, 100, 100*1024)
}

func removeFile(s *stack, p *domain.Photo) error {
	return os.Remove(filepath.Join(s.dir, p.StorageKey))
}
