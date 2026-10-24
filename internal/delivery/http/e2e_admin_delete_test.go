package httpapi_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// brokenDeletes mensimulasikan SSD yang gagal menghapus file tertentu.
type brokenDeletes struct {
	usecase.PhotoStore
	mu   sync.Mutex
	keys map[string]bool
}

func (b *brokenDeletes) Delete(ctx context.Context, key string) error {
	b.mu.Lock()
	bad := b.keys[key]
	b.mu.Unlock()
	if bad {
		return domain.Unavailable("STORAGE_UNAVAILABLE", "simulasi SSD error")
	}
	return b.PhotoStore.Delete(ctx, key)
}

func (s *stack) storageKey(t *testing.T, photoID uuid.UUID) string {
	t.Helper()
	var key string
	if err := s.pool.QueryRow(context.Background(), `SELECT storage_key FROM photos WHERE id = $1`, photoID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}

func (s *stack) onDisk(key string) bool {
	_, err := os.Stat(filepath.Join(s.dir, key))
	return err == nil
}

func TestE2EAdminDeletePhotoAndSubmission(t *testing.T) {
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000})
	admin := st.adminLogin(t)
	sub := st.submitted(t, 3)
	keys := []string{st.storageKey(t, sub.Photo[0]), st.storageKey(t, sub.Photo[1]), st.storageKey(t, sub.Photo[2])}
	tok := map[string]string{"X-Edit-Token": sub.Token}

	resp, m := admin.send(t, "DELETE", "/api/v1/admin/photos/"+sub.Photo[1].String(), "")
	if resp.StatusCode != 200 || m["status"] != "deleted" || m["photos_deleted"] != 1.0 || m["submission_deleted"] != false ||
		resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("hapus foto: %d %v", resp.StatusCode, m)
	}
	if st.onDisk(keys[1]) || !st.onDisk(keys[0]) {
		t.Fatal("hanya file foto yang dihapus yang hilang dari SSD")
	}
	if v, _, ids := st.readVersion(t, sub); v != 2 || len(ids) != 2 || ids[0] != sub.Photo[0] || ids[1] != sub.Photo[2] {
		t.Fatalf("pemilik melihat: v=%d %v", v, ids)
	}
	// pemilik dengan versi lama → 409
	if e := st.edit(t, sub.ID, sub.Token, editBody(1, sub.Photo[0], sub.Photo[2])); e.Status != 409 {
		t.Fatalf("edit versi lama: %+v", e)
	}

	resp, m = admin.send(t, "DELETE", "/api/v1/admin/submissions/"+sub.ID.String(), "")
	if resp.StatusCode != 200 || m["status"] != "deleted" || m["photos_deleted"] != 2.0 || m["submission_deleted"] != true ||
		m["annotations_deleted"] != 2.0 {
		t.Fatalf("hapus submission: %d %v", resp.StatusCode, m)
	}
	for _, k := range keys {
		if st.onDisk(k) {
			t.Fatalf("file %s masih ada", k)
		}
	}
	for _, p := range []string{"/api/v1/submissions/" + sub.ID.String(), "/api/v1/photos/" + sub.Photo[0].String() + "/url"} {
		if code, _, _ := st.get(t, p, tok); code != 404 {
			t.Fatalf("pemilik %s setelah hapus: %d", p, code)
		}
	}
	if code, _, _ := admin.get(t, "/api/v1/admin/submissions/"+sub.ID.String(), nil); code != 404 {
		t.Fatalf("admin detail setelah hapus: %d", code)
	}
	if e := st.edit(t, sub.ID, sub.Token, editBody(2, sub.Photo[0])); e.Status != 404 {
		t.Fatalf("edit setelah hapus: %+v", e)
	}
	_, _, body := admin.get(t, "/api/v1/admin/export", nil)
	if strings.Contains(string(body), sub.ID.String()) {
		t.Fatal("export tidak boleh memuat submission yang dihapus")
	}
	for _, p := range []string{"/api/v1/admin/submissions/" + sub.ID.String(), "/api/v1/admin/submissions/bukan-uuid",
		"/api/v1/admin/photos/" + uuid.NewString(), "/api/v1/admin/photos/" + sub.Photo[0].String()} {
		if resp, _ := admin.send(t, "DELETE", p, ""); resp.StatusCode != 404 {
			t.Fatalf("DELETE %s: %d", p, resp.StatusCode)
		}
	}
	// tanpa token admin
	other := st.submitted(t, 1)
	if resp, _ := st.send(t, "DELETE", "/api/v1/admin/submissions/"+other.ID.String(), ""); resp.StatusCode != 401 {
		t.Fatalf("tanpa token: %d", resp.StatusCode)
	}
}

func TestE2EAdminDeleteBulkAndCleanupPending(t *testing.T) {
	broken := &brokenDeletes{keys: map[string]bool{}}
	st := newStack(t, stackOpts{admin: testAdmin(t), adminPerMin: 1_000_000, deleteStore: func(s usecase.PhotoStore) usecase.PhotoStore {
		broken.PhotoStore = s
		return broken
	}})
	admin := st.adminLogin(t)
	a, b := st.submitted(t, 1), st.submitted(t, 2)
	broken.keys[st.storageKey(t, b.Photo[1])] = true

	missing := uuid.NewString()
	resp, m := admin.send(t, "POST", "/api/v1/admin/submissions/delete", `{"ids":["`+a.ID.String()+`","`+missing+`","`+b.ID.String()+`"]}`)
	results, _ := m["results"].([]any)
	if resp.StatusCode != 200 || len(results) != 3 {
		t.Fatalf("hapus massal: %d %v", resp.StatusCode, m)
	}
	want := []string{"deleted", "not_found", "cleanup_pending"}
	for i, r := range results {
		if r.(map[string]any)["status"] != want[i] {
			t.Fatalf("hasil[%d] = %v", i, r)
		}
	}
	// data b tersembunyi walau sebagian file menunggu cleanup
	if code, _, _ := st.get(t, "/api/v1/submissions/"+b.ID.String(), map[string]string{"X-Edit-Token": b.Token}); code != 404 {
		t.Fatalf("submission cleanup_pending terlihat: %d", code)
	}
	for _, body := range []string{`{"ids":[]}`, `{"ids":["` + a.ID.String() + `","` + a.ID.String() + `"]}`, `{"ids":["x"]}`} {
		resp, m := admin.send(t, "POST", "/api/v1/admin/submissions/delete", body)
		if resp.StatusCode != 422 && resp.StatusCode != 400 {
			t.Fatalf("%s: %d %v", body, resp.StatusCode, m)
		}
	}
	if resp, m := admin.send(t, "POST", "/api/v1/admin/submissions/delete", `{"ids":[]}`); errField(m) != "INVALID_IDS" {
		t.Fatalf("ids kosong: %d %v", resp.StatusCode, m)
	}

	// satu foto dengan SSD error → 202 cleanup_pending
	c := st.submitted(t, 1)
	broken.keys[st.storageKey(t, c.Photo[0])] = true
	resp, m = admin.send(t, "DELETE", "/api/v1/admin/submissions/"+c.ID.String(), "")
	if resp.StatusCode != 202 || m["status"] != "cleanup_pending" || m["submission_deleted"] != true {
		t.Fatalf("SSD error: %d %v", resp.StatusCode, m)
	}
}
