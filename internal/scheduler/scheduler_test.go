package scheduler_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"photo-annotated/internal/scheduler"
)

func newLog() (*slog.Logger, *bytes.Buffer, *sync.Mutex) {
	var buf bytes.Buffer
	var mu sync.Mutex
	return slog.New(slog.NewTextHandler(&lockedWriter{&buf, &mu}, nil)), &buf, &mu
}

type lockedWriter struct {
	b  *bytes.Buffer
	mu *sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func run(t *testing.T, first, every time.Duration, fn func(ctx context.Context) error) (stop func(), done chan struct{}, logs func() string) {
	t.Helper()
	log, buf, mu := newLog()
	ctx, cancel := context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Every(ctx, log, "job", first, every, fn)
	}()
	t.Cleanup(cancel)
	return cancel, done, func() string { mu.Lock(); defer mu.Unlock(); return buf.String() }
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timeout menunggu: %s", what)
}

func TestEveryRunsRepeatedlyAndStopsOnCancel(t *testing.T) {
	var n atomic.Int32
	stop, done, _ := run(t, time.Millisecond, 5*time.Millisecond, func(context.Context) error { n.Add(1); return nil })
	waitFor(t, func() bool { return n.Load() >= 4 }, "4 putaran")
	stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Every harus kembali setelah ctx dibatalkan")
	}
	after := n.Load()
	time.Sleep(30 * time.Millisecond)
	if n.Load() != after {
		t.Fatal("tidak boleh ada putaran baru setelah dibatalkan")
	}
}

func TestEveryHonorsFirstDelay(t *testing.T) {
	var n atomic.Int32
	start := time.Now()
	var firstAt atomic.Int64
	_, _, _ = run(t, 80*time.Millisecond, time.Hour, func(context.Context) error {
		if n.Add(1) == 1 {
			firstAt.Store(int64(time.Since(start)))
		}
		return nil
	})
	time.Sleep(30 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatal("fn tidak boleh jalan sebelum jeda pertama")
	}
	waitFor(t, func() bool { return n.Load() == 1 }, "putaran pertama")
	if time.Duration(firstAt.Load()) < 70*time.Millisecond {
		t.Fatalf("putaran pertama terlalu cepat: %v", time.Duration(firstAt.Load()))
	}
}

func TestEveryNeverOverlaps(t *testing.T) {
	var running, maxRunning, total atomic.Int32
	_, _, _ = run(t, time.Millisecond, time.Millisecond, func(context.Context) error {
		cur := running.Add(1)
		for {
			m := maxRunning.Load()
			if cur <= m || maxRunning.CompareAndSwap(m, cur) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond) // lebih lama daripada interval
		running.Add(-1)
		total.Add(1)
		return nil
	})
	waitFor(t, func() bool { return total.Load() >= 3 }, "3 putaran")
	if maxRunning.Load() != 1 {
		t.Fatalf("putaran tumpang tindih: maksimum serentak %d", maxRunning.Load())
	}
}

func TestEverySurvivesPanicAndLogsErrors(t *testing.T) {
	var n atomic.Int32
	_, _, logs := run(t, time.Millisecond, 3*time.Millisecond, func(context.Context) error {
		switch n.Add(1) {
		case 1:
			panic("meledak di putaran 1")
		case 2:
			return errors.New("gagal di putaran 2")
		}
		return nil
	})
	waitFor(t, func() bool { return n.Load() >= 4 }, "pekerjaan tetap berjalan setelah panic dan error")
	out := logs()
	if !strings.Contains(out, "job panic") || !strings.Contains(out, "meledak di putaran 1") || !strings.Contains(out, "stack=") {
		t.Fatalf("panic harus tercatat dengan stack:\n%s", out)
	}
	if !strings.Contains(out, "job error") || !strings.Contains(out, "gagal di putaran 2") {
		t.Fatalf("error harus tercatat:\n%s", out)
	}
}

func TestEveryDoesNotLogErrorCausedByShutdown(t *testing.T) {
	started := make(chan struct{})
	stop, done, logs := run(t, time.Millisecond, time.Hour, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err() // pembatalan karena shutdown bukan kegagalan pekerjaan
	})
	<-started
	stop()
	<-done
	if strings.Contains(logs(), "job error") {
		t.Fatalf("error akibat shutdown tidak boleh dicatat sebagai kegagalan:\n%s", logs())
	}
}

func TestEveryPanicsOnInvalidInterval(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("every <= 0 harus panic")
		}
	}()
	log, _, _ := newLog()
	scheduler.Every(context.Background(), log, "x", 0, 0, func(context.Context) error { return nil })
}
