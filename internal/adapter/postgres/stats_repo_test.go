package postgres_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/postgres"
	"photo-annotated/internal/domain"
)

func TestStatsCounts(t *testing.T) {
	r := newAuthRig(t)
	stats := postgres.NewStatsRepo(r.pool)
	if c, err := stats.Counts(ctx, now().Add(-24*time.Hour)); err != nil || c.PhotosPending != 0 || c.OTPCreated24h != 0 {
		t.Fatalf("kosong: %+v %v", c, err)
	}

	for i := 0; i < 2; i++ {
		id := uuid.New()
		if _, err := r.pool.Exec(ctx, `INSERT INTO photos (id, storage_key, content_type, size_bytes, width, height)
			VALUES ($1, $2, 'image/webp', 10, 10, 10)`, id, "2026/10/24/"+id.String()+".webp"); err != nil {
			t.Fatal(err)
		}
	}
	// lima nomor: queued, sending, sent, failed, dan satu yang dibuat 2 hari lalu
	statuses := []string{"queued", "sending", "sent", "failed", "old"}
	for _, st := range statuses {
		c := newOTP(r.user(t))
		if err := r.otps.Replace(ctx, c); err != nil {
			t.Fatal(err)
		}
		switch st {
		case "sending":
			_, _ = r.pool.Exec(ctx, `UPDATE otp_challenges SET status='sending' WHERE id=$1`, c.ID)
		case "sent":
			_, _ = r.pool.Exec(ctx, `UPDATE otp_challenges SET status='sent', sent_at=now(), expires_at=now()+interval '5 minutes' WHERE id=$1`, c.ID)
		case "failed":
			_, _ = r.pool.Exec(ctx, `UPDATE otp_challenges SET status='failed', failure_reason=$2 WHERE id=$1`, c.ID, domain.OTPFailSendFailed)
		case "old":
			r.setOTPCreated(t, c.ID, now().Add(-48*time.Hour))
			_, _ = r.pool.Exec(ctx, `UPDATE otp_challenges SET status='expired' WHERE id=$1`, c.ID)
		}
	}
	c, err := stats.Counts(ctx, now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := struct{ pending, queued, created, sent, failed int }{2, 2, 4, 1, 1}
	if c.PhotosPending != want.pending || c.OTPQueued != want.queued || c.OTPCreated24h != want.created ||
		c.OTPSent24h != want.sent || c.OTPFailed24h != want.failed {
		t.Fatalf("counts = %+v, ingin %+v", c, want)
	}
}
