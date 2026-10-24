package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	httpapi "photo-annotated/internal/delivery/http"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

const e2eSecret = "0123456789abcdef0123456789abcdef0123456789abcdef"

// e2eRouter merakit router dengan Postgres, localfs, dan signer sungguhan.
func e2eRouter(t *testing.T, minFree int64) (http.Handler, *postgres.PhotoRepo, *signer.Signer, string) {
	t.Helper()
	pool := pgtest.Pool(t)
	repo := postgres.NewPhotoRepo(pool)
	dir := t.TempDir()
	store, err := localfs.New(localfs.Options{Root: dir, MinFreeBytes: minFree})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sg, err := signer.New(e2eSecret)
	if err != nil {
		t.Fatal(err)
	}
	uc := usecase.NewPresign(repo, store, sg, usecase.SystemClock{}, usecase.PresignConfig{
		PublicBaseURL: "https://api.example.com", MaxUploadBytes: 2 << 20, UploadURLTTL: 10 * time.Minute,
	})
	return httpapi.NewRouter(httpapi.Deps{
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), TrustedProxies: []netip.Prefix{},
		DB: pool, AllowedOrigins: []string{"https://app.example.com"}, Presigner: uc, PresignPerMin: 1000,
	}), repo, sg, dir
}

func TestE2EPresignCreatesPendingPhotoWithValidSignature(t *testing.T) {
	h, repo, sg, _ := e2eRouter(t, 0)
	w := postPresign(h, goodBody, "application/json")
	if w.Code != 201 {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		PhotoID uuid.UUID `json:"photo_id"`
		Upload  struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"upload"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// baris foto benar-benar ada di Postgres, berstatus pending
	p, err := repo.Get(context.Background(), resp.PhotoID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != domain.PhotoPending || p.ContentType != "image/webp" || p.SizeBytes != 412345 ||
		p.Width != 1920 || p.Height != 1440 || !domain.IsValidStorageKey(p.StorageKey) {
		t.Fatalf("foto di DB salah: %+v", p)
	}

	// URL menunjuk ke API sendiri dan tanda tangannya sah untuk foto ini
	u, err := url.Parse(resp.Upload.URL)
	if err != nil || u.Host != "api.example.com" || u.Path != "/api/v1/uploads/"+resp.PhotoID.String() {
		t.Fatalf("URL salah: %s (%v)", resp.Upload.URL, err)
	}
	exp, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if !sg.VerifyUpload(resp.PhotoID, exp, "image/webp", 412345, u.Query().Get("sig")) {
		t.Fatal("tanda tangan di URL harus sah")
	}
	if d := time.Until(time.Unix(exp, 0)); d < 9*time.Minute || d > 10*time.Minute+time.Second {
		t.Fatalf("masa berlaku URL = %v, want ±10 menit", d)
	}
}

func TestE2EPresignRefusedWhenStorageFullAndCreatesNoRow(t *testing.T) {
	h, _, _, _ := e2eRouter(t, 1<<62) // sisa ruang mustahil terpenuhi
	w := postPresign(h, goodBody, "application/json")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "STORAGE_FULL") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestE2EPresignValidationDoesNotTouchDB(t *testing.T) {
	h, _, _, _ := e2eRouter(t, 0)
	w := postPresign(h, `{"content_type":"image/png","size_bytes":10,"width":10,"height":10}`, "application/json")
	if w.Code != 422 || !strings.Contains(w.Body.String(), "INVALID_CONTENT_TYPE") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
