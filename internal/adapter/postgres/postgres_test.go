package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/testutil/pgtest"
)

func TestNewPoolPingsAndRespectsSettings(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL kosong")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := postgres.NewPool(ctx, url, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if pool.Config().MaxConns != 3 {
		t.Fatalf("MaxConns = %d", pool.Config().MaxConns)
	}
	var app string
	if err := pool.QueryRow(ctx, "select current_setting('application_name')").Scan(&app); err != nil || app != "photo-annotated" {
		t.Fatalf("application_name = %q (%v)", app, err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNewPoolFailsFast(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := postgres.NewPool(ctx, "bukan url", 3); err == nil {
		t.Fatal("URL rusak harus gagal")
	}
	if _, err := postgres.NewPool(ctx, "postgres://u:p@127.0.0.1:1/db?sslmode=disable", 3); err == nil {
		t.Fatal("database tak terjangkau harus gagal saat startup")
	}
	if _, err := postgres.NewPool(ctx, "postgres://u:p@127.0.0.1:1/db", 0); err == nil {
		t.Fatal("maxConns 0 harus ditolak")
	}
}

func TestPoolClosedFailsPing(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if err := pool.Ping(ctx); err == nil {
		t.Fatal("ping pada pool tertutup harus gagal (dasar /readyz 503)")
	}
}

func TestMigrateUpDownStatus(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL kosong")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pakai pgtest.Pool untuk membuat schema kosong lalu mengamati migrasi lewat koneksi sendiri.
	pool := pgtest.Pool(t) // sudah ter-migrate penuh oleh helper
	var schema string
	if err := pool.QueryRow(ctx, "select current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}

	db, err := postgres.OpenSQL(url + sep(url) + "search_path=" + schema)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	st, err := postgres.MigrateStatus(ctx, db)
	if err != nil || len(st) == 0 {
		t.Fatalf("status: %v %v", st, err)
	}
	for _, s := range st {
		if !s.Applied {
			t.Fatalf("migrasi %d belum diterapkan setelah up", s.Version)
		}
	}

	if n, err := postgres.MigrateUp(ctx, db); err != nil || n != 0 {
		t.Fatalf("up kedua harus idempoten: n=%d err=%v", n, err)
	}
	v, err := postgres.MigrateDown(ctx, db)
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	st, _ = postgres.MigrateStatus(ctx, db)
	for _, s := range st {
		if s.Version == v && s.Applied {
			t.Fatalf("migrasi %d seharusnya sudah dibatalkan", v)
		}
	}
	if n, err := postgres.MigrateUp(ctx, db); err != nil || n < 1 {
		t.Fatalf("up ulang setelah down: n=%d err=%v", n, err)
	}
}

func sep(url string) string {
	for _, c := range url {
		if c == '?' {
			return "&"
		}
	}
	return "?"
}
