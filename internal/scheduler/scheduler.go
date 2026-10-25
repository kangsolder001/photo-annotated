// Package scheduler menjalankan pekerjaan berkala di dalam proses API.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

// Every menjalankan fn pertama kali setelah first, lalu setiap every (dihitung dari selesainya
// putaran sebelumnya, jadi dua putaran tidak pernah tumpang tindih). Panic di fn ditangkap dan
// dicatat; pekerjaan tetap berjalan pada putaran berikutnya. Kembali saat ctx dibatalkan; fn yang
// sedang berjalan menerima ctx yang sama sehingga ikut berhenti.
func Every(ctx context.Context, log *slog.Logger, name string, first, every time.Duration, fn func(ctx context.Context) error) {
	if every <= 0 {
		panic("scheduler: every harus > 0")
	}
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		runOnce(ctx, log, name, fn)
		timer.Reset(every)
	}
}

func runOnce(ctx context.Context, log *slog.Logger, name string, fn func(ctx context.Context) error) {
	start := time.Now()
	defer func() {
		if rec := recover(); rec != nil {
			log.Error("job panic", "job", name, "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
		}
	}()
	if err := fn(ctx); err != nil && ctx.Err() == nil {
		log.Warn("job error", "job", name, "error", err.Error(), "duration_ms", time.Since(start).Milliseconds())
	}
}
