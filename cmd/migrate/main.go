// Command migrate menerapkan migrasi skema database (terpisah dari server API).
//
//	migrate up       terapkan semua migrasi yang belum terpasang
//	migrate down     batalkan satu migrasi terakhir
//	migrate status   tampilkan status migrasi
//
// Memakai env DATABASE_URL.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"photo-annotated/internal/adapter/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "down" && args[0] != "status") {
		return fmt.Errorf("pemakaian: migrate up|down|status (env DATABASE_URL)")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("DATABASE_URL wajib diisi")
	}
	db, err := postgres.OpenSQL(url)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	switch args[0] {
	case "up":
		n, err := postgres.MigrateUp(ctx, db)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		fmt.Printf("ok: %d migrasi diterapkan\n", n)
	case "down":
		v, err := postgres.MigrateDown(ctx, db)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		fmt.Printf("ok: migrasi versi %d dibatalkan\n", v)
	case "status":
		st, err := postgres.MigrateStatus(ctx, db)
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		for _, s := range st {
			state := "pending"
			if s.Applied {
				state = "applied"
			}
			fmt.Printf("%04d  %-8s  %s\n", s.Version, state, s.Source)
		}
	}
	return nil
}
