package usecase_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/export"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

type exportRig struct {
	pool   *pgxpool.Pool
	signer *signer.Signer
	clock  *fakeClock
}

func newExportRig(t *testing.T) *exportRig {
	t.Helper()
	sg, _ := signer.New("0123456789abcdef0123456789abcdef0123456789abcdef")
	return &exportRig{pool: pgtest.Pool(t), signer: sg, clock: &fakeClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}}
}

func (r *exportRig) uc(pageSize int) *usecase.Export {
	return usecase.NewExport(postgres.NewExportRepo(r.pool), r.signer, r.clock, usecase.ExportConfig{
		PublicBaseURL: "https://api.example.com", URLTTL: time.Hour, PageSize: pageSize,
	})
}

// seed membuat satu submission (pemilik dan IP terisi, untuk memastikan keduanya tidak bocor)
// dengan foto attached berurutan, masing-masing satu anotasi, plus satu foto pending/deleted.
func (r *exportRig) seed(t *testing.T, created time.Time, photos int) uuid.UUID {
	t.Helper()
	owner := pgtest.CreateUser(t, r.pool)
	sub := uuid.New()
	if _, err := r.pool.Exec(bg, `INSERT INTO submissions (id, owner_id, client_request_id, client_ip, created_at, updated_at)
		VALUES ($1, $2, $3, '198.51.100.77', $4, $4)`, sub, owner, uuid.New(), created); err != nil {
		t.Fatal(err)
	}
	// dimasukkan terbalik agar urutan hasil benar-benar dari ORDER BY position
	for i := photos - 1; i >= 0; i-- {
		pid := uuid.New()
		if _, err := r.pool.Exec(bg, `INSERT INTO photos (id, submission_id, owner_id, position, storage_key, content_type, size_bytes,
			width, height, uploaded_at, sha256, status) VALUES ($1,$2,$3,$4,$5,'image/webp',1000,1920,1440,now(),$6,'attached')`,
			pid, sub, owner, i, "2026/10/24/"+pid.String()+".webp", bytes.Repeat([]byte{byte(i + 1)}, 32)); err != nil {
			t.Fatal(err)
		}
		if _, err := r.pool.Exec(bg, `INSERT INTO annotations (photo_id, text, x, y, w, h) VALUES ($1, $2, 0.12, 0.30, 0.25, 0.18)`,
			pid, "kotak <"+string(rune('A'+i))+"> & teks"); err != nil {
			t.Fatal(err)
		}
	}
	for _, st := range []string{"pending", "deleted"} {
		pid := uuid.New()
		if _, err := r.pool.Exec(bg, `INSERT INTO photos (id, submission_id, owner_id, storage_key, content_type, size_bytes, width, height, status)
			VALUES ($1,$2,$3,$4,'image/webp',1000,10,10,$5)`, pid, sub, owner, "2026/10/24/"+pid.String()+".webp", st); err != nil {
			t.Fatal(err)
		}
	}
	return sub
}

func runExport(t *testing.T, uc *usecase.Export, q usecase.ExportQuery) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	enc := export.NewJSONL(&buf)
	n, err := uc.Run(bg, q, enc, enc.Flush)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("baris bukan JSON: %q", sc.Text())
		}
		rows = append(rows, m)
	}
	if n != len(rows) {
		t.Fatalf("Run melaporkan %d, tertulis %d", n, len(rows))
	}
	return rows
}

func TestExportOrderPagingAndShape(t *testing.T) {
	r := newExportRig(t)
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	s1 := r.seed(t, t0, 3)
	s2 := r.seed(t, t0, 2) // created_at sama: pemecah seri submission id
	s3 := r.seed(t, t0.Add(time.Hour), 1)

	for _, page := range []int{1, 2, 500} {
		rows := runExport(t, r.uc(page), usecase.ExportQuery{})
		if len(rows) != 6 {
			t.Fatalf("page %d: %d baris, ingin 6 (pending/deleted tidak ikut)", page, len(rows))
		}
		first, second := s1, s2
		if s2.String() < s1.String() {
			first, second = s2, s1
		}
		var subs []string
		for _, row := range rows {
			subs = append(subs, row["submission_id"].(string))
		}
		want := []string{}
		n := map[uuid.UUID]int{s1: 3, s2: 2}
		for i := 0; i < n[first]; i++ {
			want = append(want, first.String())
		}
		for i := 0; i < n[second]; i++ {
			want = append(want, second.String())
		}
		want = append(want, s3.String())
		if strings.Join(subs, ",") != strings.Join(want, ",") {
			t.Fatalf("page %d: urutan salah\n%v\n%v", page, subs, want)
		}
	}

	rows := runExport(t, r.uc(500), usecase.ExportQuery{})
	row := rows[0]
	keys := []string{}
	for k := range row {
		keys = append(keys, k)
	}
	if len(row) != 5 || row["created_at"] != "2026-10-20T08:00:00Z" {
		t.Fatalf("kunci baris = %v", keys)
	}
	img := row["image"].(map[string]any)
	if len(img) != 5 || img["url"] != nil || img["width"] != 1920.0 || len(img["sha256"].(string)) != 64 ||
		!strings.HasPrefix(img["storage_key"].(string), "2026/10/24/") {
		t.Fatalf("image = %v", img)
	}
	anns := row["annotations"].([]any)
	a := anns[0].(map[string]any)
	if a["text"] != "kotak <A> & teks" {
		t.Fatalf("teks = %q", a["text"])
	}
	if b, _ := json.Marshal(a["bbox_px"]); string(b) != "[230,432,480,259]" {
		t.Fatalf("bbox_px = %s (contoh spec: [230,432,480,259])", b)
	}
	if b, _ := json.Marshal(a["bbox_norm"]); string(b) != "[0.12,0.3,0.25,0.18]" {
		t.Fatalf("bbox_norm = %s", b)
	}
	// tanpa data pribadi
	raw, _ := json.Marshal(rows)
	for _, bad := range []string{"owner", "198.51.100.77", "client_ip", "Pengguna Uji", "uji@example.com", `"phone"`, `"email"`} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("export memuat %q", bad)
		}
	}
}

func TestExportFiltersAndURLs(t *testing.T) {
	r := newExportRig(t)
	t0 := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	r.seed(t, t0, 1)
	mid := r.seed(t, t0.Add(time.Hour), 2)
	r.seed(t, t0.Add(2*time.Hour), 1)

	from, to := t0.Add(time.Hour), t0.Add(2*time.Hour)
	rows := runExport(t, r.uc(500), usecase.ExportQuery{From: &from, To: &to, IncludeURLs: true})
	if len(rows) != 2 || rows[0]["submission_id"] != mid.String() {
		t.Fatalf("filter [from,to): %d baris", len(rows))
	}
	u := rows[0]["image"].(map[string]any)["url"].(string)
	pid := uuid.MustParse(rows[0]["photo_id"].(string))
	prefix := "https://api.example.com/api/v1/photos/" + pid.String() + "/content?exp="
	if !strings.HasPrefix(u, prefix) {
		t.Fatalf("url = %s", u)
	}
	exp := r.clock.t.Add(time.Hour).Unix()
	sig := u[strings.Index(u, "sig=")+4:]
	if !r.signer.VerifyView(pid, exp, sig) {
		t.Fatal("tanda tangan URL export tidak valid / umur bukan 1 jam")
	}
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

func TestExportTenThousandPhotosStableMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("lambat")
	}
	r := newExportRig(t)
	if _, err := r.pool.Exec(bg, `
		INSERT INTO submissions (id, client_request_id, created_at, updated_at)
		SELECT gen_random_uuid(), gen_random_uuid(), now() - (g || ' seconds')::interval, now() FROM generate_series(1, 2000) g;
		INSERT INTO photos (id, submission_id, position, storage_key, content_type, size_bytes, width, height, uploaded_at, sha256, status)
		SELECT gen_random_uuid(), s.id, p, 'k/' || gen_random_uuid() || '.webp', 'image/webp', 1000, 1920, 1440, now(),
		       decode(repeat('ab', 32), 'hex'), 'attached'
		  FROM submissions s, generate_series(0, 4) p;
		INSERT INTO annotations (photo_id, text, x, y, w, h)
		SELECT id, repeat('teks anotasi panjang ', 20), 0.1, 0.1, 0.5, 0.5 FROM photos, generate_series(1, 3);`); err != nil {
		t.Fatal(err)
	}
	// heap hidup (setelah GC) diukur di akhir setiap halaman: harus datar, tidak tumbuh seiring baris
	var (
		ms        runtime.MemStats
		low, high uint64
		pages     int
	)
	w := &countingWriter{}
	enc := export.NewJSONL(w)
	repo := &pageCounter{ExportRepo: postgres.NewExportRepo(r.pool)}
	uc := usecase.NewExport(repo, r.signer, r.clock, usecase.ExportConfig{PublicBaseURL: "https://x", URLTTL: time.Hour, PageSize: 500})
	n, err := uc.Run(bg, usecase.ExportQuery{}, enc, func() error {
		runtime.GC()
		runtime.ReadMemStats(&ms)
		if pages == 0 || ms.HeapAlloc < low {
			low = ms.HeapAlloc
		}
		high = max(high, ms.HeapAlloc)
		pages++
		return enc.Flush()
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 10_000 {
		t.Fatalf("n = %d", n)
	}
	// Bukti struktural: tidak pernah lebih dari satu halaman (500 foto) yang dibaca sekaligus.
	if repo.maxPage > 500 || repo.calls != 21 {
		t.Fatalf("halaman terbesar %d, panggilan %d", repo.maxPage, repo.calls)
	}
	// Pengaman tambahan: heap hidup tidak boleh tumbuh sebesar output (menyimpan semua baris di memori
	// akan menambah > 30 MB). Pengukuran heap seluruh proses berisik (-race, goroutine test lain),
	// jadi ambangnya longgar.
	growth := high - low
	t.Logf("10.000 foto, %d halaman, %d MB output, selisih heap hidup antar halaman %.2f MB", pages, w.n>>20, float64(growth)/(1<<20))
	if growth > 16<<20 || w.n < 10_000*400 {
		t.Fatalf("heap tumbuh %d byte untuk output %d byte: tidak stabil", growth, w.n)
	}
}

// pageCounter mencatat ukuran halaman terbesar yang dibaca dari repo.
type pageCounter struct {
	usecase.ExportRepo
	calls, maxPage int
}

func (p *pageCounter) ExportPage(ctx context.Context, after *usecase.ExportCursor, from, to *time.Time, limit int) ([]usecase.ExportPhoto, error) {
	rows, err := p.ExportRepo.ExportPage(ctx, after, from, to, limit)
	p.calls++
	p.maxPage = max(p.maxPage, len(rows))
	return rows, err
}
