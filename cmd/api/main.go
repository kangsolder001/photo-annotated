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
	"syscall"
	"time"

	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/config"
	httpapi "photo-annotated/internal/delivery/http"
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

	router := httpapi.NewRouter(httpapi.Deps{
		Logger: log, TrustedProxies: trusted, DB: pool, AllowedOrigins: cfg.App.AllowedOrigins,
		Presigner: presign, PresignPerMin: cfg.Limits.RateLimitPerMin.Presign,
	})
	srv := httpapi.NewServer(cfg.App.HTTPAddr, router)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
	log.Info("server stopped")
	return nil
}
