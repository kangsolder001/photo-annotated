package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/pgtest"
	"photo-annotated/internal/usecase"
)

type authCleanRig struct {
	pool     *pgxpool.Pool
	users    *postgres.UserRepo
	otps     *postgres.OTPRepo
	sessions *postgres.SessionRepo
	now      time.Time
	step     *usecase.AuthCleanup
}

func newAuthCleanRig(t *testing.T, batch int) *authCleanRig {
	t.Helper()
	pool := pgtest.Pool(t)
	r := &authCleanRig{
		pool: pool, users: postgres.NewUserRepo(pool), otps: postgres.NewOTPRepo(pool), sessions: postgres.NewSessionRepo(pool),
		now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
	}
	r.step = usecase.NewAuthCleanup(r.users, r.otps, r.sessions, usecase.AuthCleanupConfig{
		PendingUserTTL: 24 * time.Hour, OTPRetention: 24 * time.Hour, SessionGrace: 7 * 24 * time.Hour, Batch: batch,
	})
	return r
}

var authPhoneSeq = 0

func (r *authCleanRig) newUser(t *testing.T, age time.Duration, active bool) *domain.User {
	t.Helper()
	authPhoneSeq++
	phone := "62899" + string([]byte{byte('0' + authPhoneSeq/1000%10), byte('0' + authPhoneSeq/100%10), byte('0' + authPhoneSeq/10%10), byte('0' + authPhoneSeq%10)}) + "123"
	u, _, err := r.users.FindOrCreatePending(bg, &domain.User{ID: uuid.New(), Name: "A", Email: "a@b.co", Phone: phone, CreatedAt: r.now.Add(-age)})
	if err != nil {
		t.Fatal(err)
	}
	if active {
		if err := r.users.Activate(bg, u.ID, r.now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func (r *authCleanRig) challenge(t *testing.T, u *domain.User, status string, age time.Duration) uuid.UUID {
	t.Helper()
	c := &domain.OTPChallenge{ID: uuid.New(), UserID: u.ID, Phone: u.Phone, CodeHash: bytes.Repeat([]byte{1}, 32), Purpose: domain.OTPPurposeReset,
		Status: domain.OTPQueued, NextAttemptAt: r.now, CreatedAt: r.now.Add(-age)}
	if err := r.otps.Replace(bg, c); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(bg, `UPDATE otp_challenges SET status=$2, created_at=$3 WHERE id=$1`, c.ID, status, r.now.Add(-age)); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (r *authCleanRig) session(t *testing.T, u *domain.User, h byte, expiresIn time.Duration, revokedAgo *time.Duration) []byte {
	t.Helper()
	exp := r.now.Add(expiresIn)
	var rev *time.Time
	if revokedAgo != nil {
		x := r.now.Add(-*revokedAgo)
		rev = &x
	}
	hash := bytes.Repeat([]byte{h}, 32)
	s := &domain.Session{ID: uuid.New(), UserID: u.ID, TokenHash: hash, CreatedAt: exp.Add(-90 * 24 * time.Hour), ExpiresAt: exp, RevokedAt: rev}
	if err := r.sessions.Create(bg, s); err != nil {
		t.Fatal(err)
	}
	return hash
}

func (r *authCleanRig) userExists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	_, err := r.users.Get(bg, id)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func (r *authCleanRig) otpExists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	_, err := r.otps.Get(bg, id)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func (r *authCleanRig) sessionExists(t *testing.T, hash []byte) bool {
	t.Helper()
	_, err := r.sessions.GetByTokenHash(bg, hash)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestAuthCleanupDeletesOnlyWhatIsDueAndKeepsTheRest(t *testing.T) {
	r := newAuthCleanRig(t, 100)
	hour := time.Hour
	oldPending := r.newUser(t, 25*hour, false)
	youngPending := r.newUser(t, hour, false)
	atBoundary := r.newUser(t, 24*hour, false) // tepat pada TTL: belum basi
	oldActive := r.newUser(t, 500*hour, true)

	oldConsumed := r.challenge(t, oldActive, "consumed", 25*hour)
	oldFailed := r.challenge(t, r.newUser(t, hour, false), "failed", 30*hour)
	youngConsumed := r.challenge(t, oldActive, "consumed", hour)
	oldQueued := r.challenge(t, youngPending, "queued", 40*hour) // masih berjalan: jangan disentuh

	revokedAgo8d, revokedAgo1d := 8*24*hour, 24*hour
	expired8d := r.session(t, oldActive, 1, -8*24*hour, nil)
	expired1d := r.session(t, oldActive, 2, -24*hour, nil) // dalam masa tenggang 7 hari
	revoked8d := r.session(t, oldActive, 3, 24*hour, &revokedAgo8d)
	revoked1d := r.session(t, oldActive, 4, 24*hour, &revokedAgo1d)
	active := r.session(t, oldActive, 5, 24*hour, nil)

	n, err := r.step.Run(bg, r.now)
	if err != nil {
		t.Fatal(err)
	}
	// 2 challenge (consumed lama + failed lama) + 1 pengguna pending lama (+ challenge-nya lewat CASCADE
	// tidak dihitung) + 2 sesi
	if n != 5 {
		t.Fatalf("jumlah baris terhapus = %d, want 5", n)
	}
	for name, ok := range map[string]bool{
		"pengguna pending lama":   r.userExists(t, oldPending.ID),
		"challenge consumed lama": r.otpExists(t, oldConsumed),
		"challenge failed lama":   r.otpExists(t, oldFailed),
		"sesi kedaluwarsa 8 hari": r.sessionExists(t, expired8d),
		"sesi dicabut 8 hari":     r.sessionExists(t, revoked8d),
	} {
		if ok {
			t.Errorf("%s harus terhapus", name)
		}
	}
	for name, ok := range map[string]bool{
		"pengguna pending muda":       r.userExists(t, youngPending.ID),
		"pengguna pending tepat TTL":  r.userExists(t, atBoundary.ID),
		"pengguna aktif":              r.userExists(t, oldActive.ID),
		"challenge consumed muda":     r.otpExists(t, youngConsumed),
		"challenge queued (berjalan)": r.otpExists(t, oldQueued),
		"sesi kedaluwarsa 1 hari":     r.sessionExists(t, expired1d),
		"sesi dicabut 1 hari":         r.sessionExists(t, revoked1d),
		"sesi aktif":                  r.sessionExists(t, active),
	} {
		if !ok {
			t.Errorf("%s tidak boleh terhapus", name)
		}
	}
	// idempoten
	if n, err := r.step.Run(bg, r.now); err != nil || n != 0 {
		t.Fatalf("putaran kedua: n=%d err=%v", n, err)
	}
}

func TestAuthCleanupProcessesMoreThanOneBatch(t *testing.T) {
	r := newAuthCleanRig(t, 2)
	var sessions [][]byte
	owner := r.newUser(t, 500*time.Hour, true)
	for i := 0; i < 7; i++ {
		sessions = append(sessions, r.session(t, owner, byte(20+i), -10*24*time.Hour, nil))
	}
	for i := 0; i < 5; i++ {
		r.newUser(t, 30*time.Hour, false)
	}
	n, err := r.step.Run(bg, r.now)
	if err != nil || n != 12 {
		t.Fatalf("n=%d err=%v, want 12", n, err)
	}
	for _, h := range sessions {
		if r.sessionExists(t, h) {
			t.Fatal("semua sesi kedaluwarsa harus hilang")
		}
	}
}

func TestAuthCleanupHonorsContextAndName(t *testing.T) {
	r := newAuthCleanRig(t, 10)
	if r.step.Name() != "auth" {
		t.Fatalf("Name = %q", r.step.Name())
	}
	u := r.newUser(t, 30*time.Hour, false)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := r.step.Run(ctx, r.now); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if !r.userExists(t, u.ID) {
		t.Fatal("konteks batal tidak boleh menghapus")
	}
}

func TestAuthCleanupRunsInsideCleanupRunOnce(t *testing.T) {
	r := newAuthCleanRig(t, 100)
	u := r.newUser(t, 30*time.Hour, false)
	// Cleanup memakai CleanupRepo/PhotoStore nyata dari rig foto
	base := newCleanRig(t, cleanOpts{steps: nil})
	cu := usecase.NewCleanup(base.repo, base.store, &fakeClock{t: r.now}, base.cfg, r.step)
	rep, err := cu.RunOnce(bg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Steps["auth"] != 1 || r.userExists(t, u.ID) {
		t.Fatalf("laporan: %+v", rep)
	}
}

func TestNewAuthCleanupPanicsOnInvalidBatch(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Batch 0 harus panic")
		}
	}()
	usecase.NewAuthCleanup(nil, nil, nil, usecase.AuthCleanupConfig{})
}
