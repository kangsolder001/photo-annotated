// Package pgtest menyiapkan database uji terisolasi: tiap test mendapat schema acak berisi
// seluruh migrasi, dihapus saat test selesai. Test dilewati bila TEST_DATABASE_URL kosong.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"photo-annotated/internal/adapter/postgres"
)

// Pool mengembalikan pool yang search_path-nya terkunci ke schema baru bermigrasi penuh.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL kosong; lewati test integrasi (jalankan scripts/db-tunnel.sh dan muat .env.local)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgtest: koneksi admin: %v", err)
	}
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	schema := "t_" + hex.EncodeToString(rnd[:])
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("pgtest: create schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	// migrasi ke schema ini lewat koneksi database/sql dengan search_path yang sama
	sqlDB := stdlib.OpenDB(*cfg.ConnConfig)
	if _, err := postgres.MigrateUp(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		admin.Close()
		t.Fatalf("pgtest: migrate up: %v", err)
	}
	_ = sqlDB.Close()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		admin.Close()
		t.Fatalf("pgtest: pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(cctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Logf("pgtest: drop schema %s: %v", schema, err)
		}
		admin.Close()
	})
	return pool
}

// CreateUser menyisipkan pengguna aktif dengan nomor acak dan mengembalikan id-nya. Dipakai test
// yang membutuhkan pemilik nyata (owner_id punya FK ke users).
func CreateUser(t testing.TB, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.New()
	for attempt := 0; attempt < 5; attempt++ {
		var rnd [8]byte
		_, _ = rand.Read(rnd[:])
		phone := "628"
		for _, b := range rnd {
			phone += string(rune('0' + b%10))
		}
		now := time.Now().UTC()
		_, err := pool.Exec(ctx, `INSERT INTO users (id, name, email, phone, status, phone_verified_at, created_at)
			VALUES ($1, 'Pengguna Uji', 'uji@example.com', $2, 'active', $3, $3)`, id, phone, now)
		if err == nil {
			return id
		}
	}
	t.Fatal("pgtest: gagal membuat pengguna uji")
	return uuid.Nil
}
