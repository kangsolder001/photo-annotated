// Command api menjalankan server HTTP Photo Annotated.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"photo-annotated/internal/adapter/imagecheck"
	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/config"
	httpapi "photo-annotated/internal/delivery/http"
	"photo-annotated/internal/scheduler"
	"photo-annotated/internal/usecase"
)

const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(config.Path(os.Getenv), os.Getenv)
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	trusted := make([]netip.Prefix, 0, len(cfg.App.TrustedProxies))
	for _, s := range cfg.App.TrustedProxies {
		p, err := config.ParseProxy(s) // sudah divalidasi saat load
		if err != nil {
			return err
		}
		trusted = append(trusted, p)
	}

	startCtx, cancelStart := context.WithTimeout(context.Background(), 20*time.Second)
	pool, err := postgres.NewPool(startCtx, cfg.Secrets.DatabaseURL, cfg.Database.MaxConns)
	cancelStart()
	if err != nil {
		return err // gagal cepat: orkestrator (restart policy) akan mencoba lagi
	}
	defer pool.Close()

	store, err := localfs.New(localfs.Options{
		Root:          cfg.Storage.Local.Root,
		RequireMarker: cfg.Storage.Local.RequireMarker,
		MinFreeBytes:  cfg.Storage.Local.MinFreeBytes,
	})
	if err != nil {
		return err // mis. SSD belum ter-mount (penanda hilang): gagal cepat
	}
	defer store.Close()

	urlSigner, err := signer.New(cfg.Secrets.StorageSigningSecret)
	if err != nil {
		return err
	}
	presign := usecase.NewPresign(postgres.NewPhotoRepo(pool), store, urlSigner, usecase.SystemClock{}, usecase.PresignConfig{
		PublicBaseURL:  cfg.App.PublicBaseURL,
		MaxUploadBytes: cfg.Limits.MaxUploadBytes,
		UploadURLTTL:   cfg.Storage.UploadURLTTL.Std(),
	})

	photos := postgres.NewPhotoRepo(pool)
	// Token edit anonim hanya berlaku pada auth.mode = none; pada mode otp pemilik dikenali dari akun.
	var editTokens usecase.EditTokenSigner
	if cfg.Auth.Mode == config.AuthModeNone {
		et, err := signer.NewEditTokens(cfg.Secrets.EditTokenSecret)
		if err != nil {
			return err
		}
		editTokens = et
	}
	subs := postgres.NewSubmissionRepo(pool)
	submitCfg := usecase.SubmitConfig{
		MaxPhotos:              cfg.Limits.MaxPhotosPerSubmission,
		MaxAnnotationsPerPhoto: cfg.Limits.MaxAnnotationsPerPhoto,
		MaxAnnotationTextLen:   cfg.Limits.MaxAnnotationTextLen,
	}
	txm := postgres.NewTxManager(pool)
	submit := usecase.NewSubmit(photos, subs, txm, store, editTokens, usecase.SystemClock{}, submitCfg)
	editor := usecase.NewEdit(photos, subs, txm, store, editTokens, usecase.SystemClock{},
		usecase.EditConfig{SubmitConfig: submitCfg, Window: cfg.Edit.Window.Std()})
	reader := usecase.NewReader(subs, photos, store, urlSigner, editTokens, usecase.SystemClock{}, usecase.ReadConfig{
		PublicBaseURL: cfg.App.PublicBaseURL,
		EditWindow:    cfg.Edit.Window.Std(),
		ViewURLTTL:    cfg.Storage.ViewURLTTL.Std(),
	})
	upload := usecase.NewUpload(photos, store, urlSigner, imagecheck.Inspector{}, usecase.SystemClock{},
		usecase.UploadConfig{MaxConcurrent: cfg.Limits.MaxConcurrentUploads})

	router := httpapi.NewRouter(httpapi.Deps{
		Logger: log, TrustedProxies: trusted, DB: pool, AllowedOrigins: cfg.App.AllowedOrigins,
		Presigner: presign, PresignPerMin: cfg.Limits.RateLimitPerMin.Presign,
		Reader:    reader,
		Editor:    editor,
		Submitter: submit, SubmissionsPerMin: cfg.Limits.RateLimitPerMin.Submissions,
		Uploader: upload, MaxUploadBytes: cfg.Limits.MaxUploadBytes, UploadTimeout: cfg.Limits.UploadTimeout.Std(),
	})
	srv := httpapi.NewServer(cfg.App.HTTPAddr, router)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Pekerjaan berkala (spec §8.3): pembersihan + retensi tiap cleanup_interval, sapuan file
	// yatim tiap orphan_sweep_interval. Keduanya berhenti saat ctx dibatalkan (SIGTERM).
	cleanup := usecase.NewCleanup(postgres.NewCleanupRepo(pool), store, usecase.SystemClock{}, usecase.CleanupConfig{
		RetentionDays: cfg.Retention.Days,
		PendingTTL:    cfg.Retention.PendingPhotoTTL.Std(),
		Batch:         cfg.Retention.CleanupBatch,
		TmpMaxAge:     cfg.Retention.TmpMaxAge.Std(),
		OrphanMinAge:  cfg.Retention.OrphanMinAge.Std(),
	})
	var jobs sync.WaitGroup
	jobs.Add(2)
	go func() {
		defer jobs.Done()
		scheduler.Every(ctx, log, "cleanup", time.Minute, cfg.Retention.CleanupInterval.Std(), func(ctx context.Context) error {
			rep, err := cleanup.RunOnce(ctx)
			if !rep.Empty() || err != nil {
				log.Info("cleanup", "stale_pending", rep.StalePendingClaimed, "retention_photos", rep.RetentionPhotos,
					"files_deleted", rep.FilesDeleted, "file_failures", rep.FileDeleteFailures, "photo_rows_deleted", rep.PhotoRowsDeleted,
					"submissions_deleted", rep.SubmissionsDeleted, "tmp_swept", rep.TmpSwept, "duration_ms", rep.Duration.Milliseconds())
			}
			return err
		})
	}()
	go func() {
		defer jobs.Done()
		scheduler.Every(ctx, log, "orphan-sweep", 5*time.Minute, cfg.Retention.OrphanSweepInterval.Std(), func(ctx context.Context) error {
			rep, err := cleanup.SweepOrphans(ctx)
			log.Info("orphan sweep", "scanned", rep.Scanned, "removed", rep.Removed, "failures", rep.Failures, "duration_ms", rep.Duration.Milliseconds())
			return err
		})
	}()

	errc := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", cfg.App.HTTPAddr, "env", cfg.App.Env, "auth_mode", cfg.Auth.Mode)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Info("shutdown requested")
	}

	shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	jobs.Wait() // ctx sudah dibatalkan: pekerjaan berkala berhenti di sela putaran
	log.Info("server stopped")
	return nil
}
