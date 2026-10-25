package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"photo-annotated/migrations"
)

// MigrationStatus menggambarkan satu migrasi dan apakah sudah diterapkan.
type MigrationStatus struct {
	Version int64
	Source  string
	Applied bool
}

// OpenSQL membuka *sql.DB (driver pgx) khusus untuk migrasi.
func OpenSQL(url string) (*sql.DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: DATABASE_URL tidak valid: %w", err)
	}
	return stdlib.OpenDB(*cfg.ConnConfig), nil
}

func provider(db *sql.DB) (*goose.Provider, error) {
	// Session locker mencegah dua proses menjalankan migrasi bersamaan.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
}

// MigrateUp menerapkan semua migrasi yang belum terpasang; mengembalikan jumlah yang diterapkan.
func MigrateUp(ctx context.Context, db *sql.DB) (int, error) {
	p, err := provider(db)
	if err != nil {
		return 0, err
	}
	res, err := p.Up(ctx)
	return len(res), err
}

// MigrateUpUnlocked sama dengan MigrateUp tetapi tanpa session locker. Hanya untuk schema uji yang
// terisolasi (pgtest): kunci advisory goose berlaku untuk seluruh database, sehingga paket test yang
// berjalan paralel saling menunggu dan bisa melewati batas waktu walau schema-nya berbeda.
func MigrateUpUnlocked(ctx context.Context, db *sql.DB) (int, error) {
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return 0, err
	}
	res, err := p.Up(ctx)
	return len(res), err
}

// MigrateDown membatalkan satu migrasi terakhir; mengembalikan versi yang dibatalkan.
func MigrateDown(ctx context.Context, db *sql.DB) (int64, error) {
	p, err := provider(db)
	if err != nil {
		return 0, err
	}
	res, err := p.Down(ctx)
	if err != nil {
		return 0, err
	}
	return res.Source.Version, nil
}

// MigrateStatus mengembalikan status seluruh migrasi.
func MigrateStatus(ctx context.Context, db *sql.DB) ([]MigrationStatus, error) {
	p, err := provider(db)
	if err != nil {
		return nil, err
	}
	st, err := p.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MigrationStatus, 0, len(st))
	for _, s := range st {
		out = append(out, MigrationStatus{
			Version: s.Source.Version,
			Source:  s.Source.Path,
			Applied: s.State == goose.StateApplied,
		})
	}
	return out, nil
}
