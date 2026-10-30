package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
)

func TestLabelConstraints(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := postgres.NewLabelRepo(pool)
	l := &domain.Label{Name: "Retak", Color: "#e4572e", CreatedAt: now(), UpdatedAt: now()}
	if err := repo.Create(ctx, l); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, l.ID)
	if err != nil || got.Name != "Retak" || got.Description != "" || got.Color != "#e4572e" || got.Archived() {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for name, sql := range map[string]string{
		"nama kosong":            `INSERT INTO labels (name, color) VALUES ('', '#000000')`,
		"nama 51":                `INSERT INTO labels (name, color) VALUES (repeat('a', 51), '#000000')`,
		"warna huruf besar":      `INSERT INTO labels (name, color) VALUES ('x1', '#ABCDEF')`,
		"warna tanpa #":          `INSERT INTO labels (name, color) VALUES ('x2', 'abcdef')`,
		"deskripsi kosong":       `INSERT INTO labels (name, color, description) VALUES ('x3', '#000000', '')`,
		"deskripsi 201":          `INSERT INTO labels (name, color, description) VALUES ('x4', '#000000', repeat('d', 201))`,
		"id diisi manual":        `INSERT INTO labels (id, name, color) VALUES (500, 'x5', '#000000')`,
		"nama beda kapitalisasi": `INSERT INTO labels (name, color) VALUES ('RETAK', '#000000')`,
	} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Errorf("%s harus ditolak", name)
		}
	}
	// anotasi wajib punya label atau teks lama; label yang dipakai tidak bisa dihapus (RESTRICT)
	_, photoID := pgtest.InsertLabeledAnnotation(t, pool, l.ID, "attached")
	if _, err := pool.Exec(ctx, `INSERT INTO annotations (photo_id, x, y, w, h) VALUES ($1, 0.1, 0.1, 0.2, 0.2)`, photoID); pgCode(err) != "23514" {
		t.Fatalf("anotasi tanpa label dan teks: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO annotations (photo_id, text, x, y, w, h) VALUES ($1, 'catatan lama', 0.1, 0.1, 0.2, 0.2)`, photoID); err != nil {
		t.Fatalf("anotasi teks lama tetap boleh: %v", err)
	}
	if err := repo.Delete(ctx, l.ID); codeOf(err) != "LABEL_IN_USE" {
		t.Fatalf("hapus label terpakai: %v", err)
	}
	if err := repo.Delete(ctx, 424242); err == nil {
		t.Fatal("hapus id tak ada harus gagal")
	}
}

// Down 0007 tidak boleh membuang data: gagal dengan pesan jelas bila ada anotasi berlabel.
func TestLabelsMigrationDownRefusesWhenUsed(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	pool := pgtest.Pool(t)
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var schema string
	if err := pool.QueryRow(c, "select current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	db, err := postgres.OpenSQL(url + sep(url) + "search_path=" + schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// mundur sampai 0007 menjadi migrasi terakhir
	for {
		st, err := postgres.MigrateStatus(c, db)
		if err != nil {
			t.Fatal(err)
		}
		var last int64
		for _, s := range st {
			if s.Applied && s.Version > last {
				last = s.Version
			}
		}
		if last <= 7 {
			break
		}
		if _, err := postgres.MigrateDown(c, db); err != nil {
			t.Fatal(err)
		}
	}
	l := &domain.Label{Name: "Retak", Color: "#e4572e", CreatedAt: now(), UpdatedAt: now()}
	if err := postgres.NewLabelRepo(pool).Create(ctx, l); err != nil {
		t.Fatal(err)
	}
	pgtest.InsertLabeledAnnotation(t, pool, l.ID, "attached")
	if _, err := postgres.MigrateDown(c, db); err == nil || !strings.Contains(err.Error(), "tidak bisa dibatalkan") {
		t.Fatalf("down harus gagal: %v", err)
	}
	var n int
	if err := pool.QueryRow(c, `SELECT count(*) FROM labels`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("label harus tetap ada: %d %v", n, err)
	}
}

// spec-pendataan §6: migrasi 0013 mengisi label kotak bawaan bila tabel labels kosong.
func TestSurveyMigrationSeedsDefaultLabels(t *testing.T) {
	pool := pgtest.PoolWithSeed(t)
	ls, err := postgres.NewLabelRepo(pool).ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range ls {
		names = append(names, l.Name+" "+l.Color)
	}
	if got := strings.Join(names, ","); got != "Organik #2e7d32,Anorganik #1565c0,Residu #6d4c41,B3 #c62828" {
		t.Fatalf("label bawaan: %s", got)
	}
}
