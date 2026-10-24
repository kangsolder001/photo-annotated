package usecase

import (
	"context"
	"time"
)

// AuthCleanupConfig adalah parameter pembersihan data auth (spec §8.3 langkah 4).
type AuthCleanupConfig struct {
	PendingUserTTL time.Duration // pengguna pending (OTP pertama tak pernah diverifikasi) dihapus setelah ini
	OTPRetention   time.Duration // challenge OTP selesai dihapus setelah ini
	SessionGrace   time.Duration // sesi kedaluwarsa/dicabut dihapus setelah masa tenggang ini
	Batch          int
	MaxBatches     int // 0 = default
}

// AuthCleanup adalah CleanupStep untuk data auth.
type AuthCleanup struct {
	users    UserRepo
	otps     OTPRepo
	sessions SessionRepo
	cfg      AuthCleanupConfig
}

var _ CleanupStep = (*AuthCleanup)(nil)

func NewAuthCleanup(users UserRepo, otps OTPRepo, sessions SessionRepo, cfg AuthCleanupConfig) *AuthCleanup {
	if cfg.Batch < 1 {
		panic("usecase: AuthCleanupConfig.Batch harus >= 1")
	}
	if cfg.MaxBatches < 1 {
		cfg.MaxBatches = defaultMaxBatches
	}
	return &AuthCleanup{users: users, otps: otps, sessions: sessions, cfg: cfg}
}

func (a *AuthCleanup) Name() string { return "auth" }

// Run menghapus: pengguna pending basi (challenge-nya ikut lewat CASCADE), challenge OTP lama yang
// sudah selesai, dan sesi yang kedaluwarsa/dicabut lewat masa tenggang. Mengembalikan jumlah baris.
func (a *AuthCleanup) Run(ctx context.Context, now time.Time) (int, error) {
	total := 0
	steps := []func(context.Context, int) (int, error){
		func(ctx context.Context, n int) (int, error) {
			return a.otps.DeleteBefore(ctx, now.Add(-a.cfg.OTPRetention), n)
		},
		func(ctx context.Context, n int) (int, error) {
			return a.users.DeletePendingBefore(ctx, now.Add(-a.cfg.PendingUserTTL), n)
		},
		func(ctx context.Context, n int) (int, error) {
			return a.sessions.DeleteInactiveBefore(ctx, now.Add(-a.cfg.SessionGrace), n)
		},
	}
	for _, step := range steps {
		for i := 0; i < a.cfg.MaxBatches; i++ {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			n, err := step(ctx, a.cfg.Batch)
			total += n
			if err != nil {
				return total, err
			}
			if n < a.cfg.Batch {
				break
			}
		}
	}
	return total, nil
}
