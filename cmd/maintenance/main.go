// Command maintenance menjalankan pekerjaan pemeliharaan sekali jalan, memakai config dan env yang
// sama dengan API (CONFIG_PATH, DATABASE_URL, ...):
//
//	maintenance cleanup         pembersihan foto + retensi + sisa tmp (seperti putaran job per jam)
//	maintenance sweep-orphans   sapuan file di SSD yang tidak punya baris foto
//
// Aman dijalankan bersamaan dengan API (kueri memakai SKIP LOCKED dan semua langkah idempoten).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/config"
	"photo-annotated/internal/usecase"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 || (args[0] != "cleanup" && args[0] != "sweep-orphans") {
		return fmt.Errorf("pemakaian: maintenance cleanup|sweep-orphans")
	}
	cfg, err := config.Load(config.Path(os.Getenv), os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.Secrets.DatabaseURL, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	store, err := localfs.New(localfs.Options{
		Root:          cfg.Storage.Local.Root,
		RequireMarker: cfg.Storage.Local.RequireMarker,
		MinFreeBytes:  cfg.Storage.Local.MinFreeBytes,
	})
	if err != nil {
		return err
	}
	defer store.Close()

	cleanup := usecase.NewCleanup(postgres.NewCleanupRepo(pool), store, usecase.SystemClock{}, usecase.CleanupConfig{
		RetentionDays: cfg.Retention.Days,
		PendingTTL:    cfg.Retention.PendingPhotoTTL.Std(),
		Batch:         cfg.Retention.CleanupBatch,
		TmpMaxAge:     cfg.Retention.TmpMaxAge.Std(),
		OrphanMinAge:  cfg.Retention.OrphanMinAge.Std(),
	})

	switch args[0] {
	case "cleanup":
		rep, err := cleanup.RunOnce(ctx)
		fmt.Printf("foto pending basi di-claim : %d\n", rep.StalePendingClaimed)
		fmt.Printf("foto ditandai (retensi)    : %d\n", rep.RetentionPhotos)
		fmt.Printf("file dihapus               : %d (gagal: %d)\n", rep.FilesDeleted, rep.FileDeleteFailures)
		fmt.Printf("baris foto dihapus         : %d\n", rep.PhotoRowsDeleted)
		fmt.Printf("submission dihapus         : %d\n", rep.SubmissionsDeleted)
		fmt.Printf("sisa tmp dibersihkan       : %d\n", rep.TmpSwept)
		fmt.Printf("durasi                     : %v\n", rep.Duration)
		return err
	default:
		rep, err := cleanup.SweepOrphans(ctx)
		fmt.Printf("file lama diperiksa: %d\nfile yatim dihapus : %d (gagal: %d)\ndurasi             : %v\n",
			rep.Scanned, rep.Removed, rep.Failures, rep.Duration)
		return err
	}
}
