// Package postgres berisi adapter PostgreSQL: pool koneksi, migrasi, dan repository.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	connectTimeout = 5 * time.Second
	startupPing    = 10 * time.Second
)

// NewPool membuka pool pgx dan memastikan database terjangkau (gagal cepat saat startup).
func NewPool(ctx context.Context, url string, maxConns int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: DATABASE_URL tidak valid: %w", err)
	}
	return newPool(ctx, cfg, maxConns)
}

func newPool(ctx context.Context, cfg *pgxpool.Config, maxConns int) (*pgxpool.Pool, error) {
	if maxConns < 1 {
		return nil, fmt.Errorf("postgres: maxConns harus >= 1 (dapat %d)", maxConns)
	}
	cfg.MaxConns = int32(maxConns)
	cfg.MinConns = min(2, int32(maxConns))
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "photo-annotated"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: membuat pool: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, startupPing)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: database tidak terjangkau: %w", err)
	}
	return pool, nil
}
