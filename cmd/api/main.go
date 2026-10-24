// Command api menjalankan server HTTP Photo Annotated.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/export"
	"photo-annotated/internal/adapter/imagecheck"
	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/adapter/signer"
	"photo-annotated/internal/adapter/turnstile"
	"photo-annotated/internal/adapter/wanotif"
	"photo-annotated/internal/config"
	httpapi "photo-annotated/internal/delivery/http"
	"photo-annotated/internal/delivery/http/handler"
	"photo-annotated/internal/scheduler"
	"photo-annotated/internal/usecase"
)

const (
	shutdownTimeout  = 15 * time.Second
	turnstileTimeout = 5 * time.Second
	// export dataset: URL foto berumur 1 jam (spec §5.7), 500 foto per halaman keyset
	exportURLTTL   = time.Hour
	exportPageSize = 500
	// token Turnstile berlaku 300 detik di Cloudflare; pemakaian ulang tidak boleh lebih lama
	turnstileReuseTTL = 5 * time.Minute
	// ambang kegagalan kirim berturut-turut sebelum kanal OTP dianggap tidak tersedia (spec §9.5)
	channelFailureThreshold = 3
	// dispatcher memeriksa antrean paling lambat tiap detik (juga dibangunkan saat ada challenge baru)
	dispatchIdle = time.Second
	// interval baris log "stats" (gauge dashboard)
	statsInterval = time.Minute
)

// jeda sebelum percobaan kirim ke-2 dan ke-3 (spec §4.0): kegagalan terlihat user dalam ± 25 detik
var otpRetryDelays = []time.Duration{3 * time.Second, 10 * time.Second}

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

	deps := httpapi.Deps{
		Logger: log, TrustedProxies: trusted, DB: pool, AllowedOrigins: cfg.App.AllowedOrigins,
		Presigner: presign, PresignPerMin: cfg.Limits.RateLimitPerMin.Presign,
		Reader:    reader,
		Editor:    editor,
		Submitter: submit, SubmissionsPerMin: cfg.Limits.RateLimitPerMin.Submissions,
		Uploader: upload, MaxUploadBytes: cfg.Limits.MaxUploadBytes, UploadTimeout: cfg.Limits.UploadTimeout.Std(),
	}
	if cfg.Admin.Enabled {
		tokens, err := signer.NewAdminTokens(cfg.Secrets.AdminTokenSecret)
		if err != nil {
			return err
		}
		deps.Admin = usecase.NewAdminAuth(tokens, usecase.SystemClock{}, usecase.AdminConfig{
			Username: cfg.Admin.Username, PasswordBcrypt: cfg.Admin.PasswordBcrypt, TokenTTL: cfg.Admin.TokenTTL.Std(),
		})
		deps.AdminLoginPerMin = cfg.Limits.RateLimitPerMin.AdminLogin
		deps.Exporter = usecase.NewExport(postgres.NewExportRepo(pool), urlSigner, usecase.SystemClock{}, usecase.ExportConfig{
			PublicBaseURL: cfg.App.PublicBaseURL, URLTTL: exportURLTTL, PageSize: exportPageSize,
		})
		deps.NewJSONL = func(w io.Writer) handler.ExportStream { return export.NewJSONL(w) }
	}
	if cfg.Turnstile.Enabled {
		ts, err := turnstile.New(cfg.Secrets.TurnstileSecret, "", turnstileTimeout)
		if err != nil {
			return err
		}
		deps.Turnstile = ts
		if cfg.Auth.Mode == config.AuthModeNone {
			// satu token untuk semua presign satu submission ditambah submit-nya
			deps.UploadTurnstile = turnstile.NewReusable(ts, cfg.Limits.MaxPhotosPerSubmission+1, turnstileReuseTTL)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var jobs sync.WaitGroup

	if cfg.Auth.Mode == config.AuthModeOTP {
		if err := startOTP(ctx, log, cfg, pool, &deps, &jobs); err != nil {
			return err
		}
	}
	router := httpapi.NewRouter(deps)
	srv := httpapi.NewServer(cfg.App.HTTPAddr, router)

	// Pekerjaan berkala (spec §8.3): pembersihan + retensi tiap cleanup_interval, sapuan file
	// yatim tiap orphan_sweep_interval. Keduanya berhenti saat ctx dibatalkan (SIGTERM).
	cleanup := usecase.NewCleanup(postgres.NewCleanupRepo(pool), store, usecase.SystemClock{}, usecase.CleanupConfig{
		RetentionDays: cfg.Retention.Days,
		PendingTTL:    cfg.Retention.PendingPhotoTTL.Std(),
		Batch:         cfg.Retention.CleanupBatch,
		TmpMaxAge:     cfg.Retention.TmpMaxAge.Std(),
		OrphanMinAge:  cfg.Retention.OrphanMinAge.Std(),
	})
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

	// Gauge berkala untuk dashboard (spec §12): satu baris log "stats" per menit, dibaca Loki.
	statsRepo := postgres.NewStatsRepo(pool)
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		scheduler.Every(ctx, log, "stats", 30*time.Second, statsInterval, func(ctx context.Context) error {
			return logStats(ctx, log, cfg, statsRepo, store, upload, deps.OTPChannel)
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

// startOTP merakit alur OTP (auth.mode = otp): permintaan kode, dispatcher pengiriman, dan probe
// kanal WhatsApp. Dispatcher dan probe berjalan sampai ctx dibatalkan.
func startOTP(ctx context.Context, log *slog.Logger, cfg *config.Config, pool *pgxpool.Pool, deps *httpapi.Deps, jobs *sync.WaitGroup) error {
	o := cfg.Auth.OTP
	codes, err := signer.NewOTPCodes(cfg.Secrets.OTPPepper, o.Length)
	if err != nil {
		return err
	}
	wa, err := wanotif.New(cfg.WANotif.BaseURL, cfg.Secrets.WANotifToken, cfg.WANotif.Timeout.Std())
	if err != nil {
		return err
	}
	otps := postgres.NewOTPRepo(pool)
	channel := usecase.NewChannel(channelFailureThreshold)
	phones := usecase.NewPhoneLimiter(o.ResendCooldown.Std(), o.MaxPerPhonePerHour)
	dispatcher := usecase.NewOTPDispatcher(otps, wa, codes, channel, phones, usecase.SystemClock{}, usecase.OTPDispatchConfig{
		CodeTTL: o.TTL.Std(), MessageTemplate: o.MessageTemplate, RetryDelays: otpRetryDelays,
		StuckAfter: 3 * cfg.WANotif.Timeout.Std(),
	})
	users := postgres.NewUserRepo(pool)
	sessions := postgres.NewSessionRepo(pool)
	requester := usecase.NewOTPRequest(users, otps, codes, channel, phones, usecase.SystemClock{},
		usecase.OTPRequestConfig{
			ResendCooldown: o.ResendCooldown.Std(), MaxQueue: o.MaxQueue, DailyGlobalCap: o.DailyGlobalCap,
			SendInterval: cfg.WANotif.SendInterval.Std(), CodeTTL: o.TTL.Std(), OnQueued: dispatcher.Wake,
		})
	deps.OTPRequester = requester
	deps.OTPVerifier = usecase.NewOTPVerify(requester, users, otps, sessions, postgres.NewTxManager(pool), codes, usecase.SystemClock{},
		usecase.OTPVerifyConfig{MaxAttempts: o.MaxVerifyAttempts, SessionTTL: cfg.Auth.SessionTTL.Std()})
	deps.Sessions = usecase.NewSessions(sessions, users, usecase.SystemClock{})
	deps.OTPVerifyPerMin = cfg.Limits.RateLimitPerMin.OTPVerify
	deps.OTPChannel = channel
	deps.OTPRequestPerMin = cfg.Limits.RateLimitPerMin.OTPRequest

	jobs.Add(2)
	go func() {
		defer jobs.Done()
		dispatcher.Run(ctx, dispatchIdle, func(r usecase.DispatchResult, err error) {
			logDispatch(log, r, err)
		})
	}()
	go func() {
		defer jobs.Done()
		scheduler.Every(ctx, log, "wa-probe", 0, cfg.WANotif.HealthProbeInterval.Std(), func(ctx context.Context) error {
			err := wa.Check(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if avail, changed := channel.RecordProbe(err); changed {
				logChannel(log, avail, "probe", err)
			}
			return nil // status kanal dicatat saat berubah, bukan tiap probe gagal
		})
	}()
	return nil
}

func logChannel(log *slog.Logger, available bool, source string, cause error) {
	attrs := []any{"otp_available", available, "source", source}
	if cause != nil {
		attrs = append(attrs, "cause", cause.Error())
	}
	if available {
		log.Info("otp channel available", attrs...)
	} else {
		log.Error("otp channel unavailable", attrs...)
	}
}

// logDispatch mencatat hasil dispatcher. Nomor sudah disamarkan; kode dan isi pesan tidak pernah ada di sini.
func logDispatch(log *slog.Logger, r usecase.DispatchResult, err error) {
	if err != nil {
		log.Error("otp dispatch error", "error", err.Error(), "challenge_id", r.ChallengeID)
		return
	}
	attrs := []any{"outcome", string(r.Outcome), "challenge_id", r.ChallengeID, "phone", r.Phone, "attempt", r.Attempt}
	if r.Outcome == usecase.DispatchRequeued {
		attrs = []any{"outcome", string(r.Outcome), "count", r.Requeued}
	}
	if r.Reason != "" {
		attrs = append(attrs, "failure_reason", r.Reason)
	}
	if r.Cause != nil {
		attrs = append(attrs, "cause", r.Cause.Error())
	}
	switch {
	case r.Alert:
		log.Error("otp dispatch needs operator attention", attrs...)
	case r.Outcome == usecase.DispatchFailed || r.Outcome == usecase.DispatchRetry:
		log.Warn("otp dispatch", attrs...)
	default:
		log.Info("otp dispatch", attrs...)
	}
	if r.ChannelChanged {
		logChannel(log, r.Available, "send", nil)
	}
}

// logStats mencatat gauge: foto pending, slot upload, ruang SSD, dan (mode otp) antrean serta
// hitungan OTP 24 jam terhadap daily_global_cap. Tanpa data pribadi.
func logStats(ctx context.Context, log *slog.Logger, cfg *config.Config, repo *postgres.StatsRepo, store *localfs.Store,
	upload *usecase.Upload, channel handler.ChannelStatus) error {
	c, err := repo.Counts(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	inUse, capacity := upload.Slots()
	attrs := []any{"photos_pending", c.PhotosPending, "uploads_active", inUse, "uploads_max", capacity}
	if total, free, err := store.Usage(); err == nil && total > 0 {
		attrs = append(attrs, "storage_total_bytes", total, "storage_free_bytes", free,
			"storage_used_pct", float64(total-free)*100/float64(total), "storage_min_free_bytes", cfg.Storage.Local.MinFreeBytes)
	}
	if channel != nil {
		o := cfg.Auth.OTP
		attrs = append(attrs, "otp_available", channel.Available(), "otp_queued", c.OTPQueued, "otp_max_queue", o.MaxQueue,
			"otp_created_24h", c.OTPCreated24h, "otp_sent_24h", c.OTPSent24h, "otp_failed_24h", c.OTPFailed24h,
			"otp_daily_cap", o.DailyGlobalCap)
	}
	log.Info("stats", attrs...)
	return nil
}
