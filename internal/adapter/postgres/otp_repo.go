package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type OTPRepo struct {
	base
	tx *TxManager
}

var _ usecase.OTPRepo = (*OTPRepo)(nil)

func NewOTPRepo(pool *pgxpool.Pool) *OTPRepo {
	return &OTPRepo{base: base{pool}, tx: NewTxManager(pool)}
}

const otpColumns = `id, user_id, phone, code_hash, status, failure_reason, verify_attempts, send_attempts,
	next_attempt_at, wa_message_id, client_ip, created_at, sent_at, expires_at`

func scanOTP(row pgx.Row) (*domain.OTPChallenge, error) {
	var (
		c        domain.OTPChallenge
		status   string
		failure  *string
		waMsgID  *string
		vAtt, sA int16
	)
	err := row.Scan(&c.ID, &c.UserID, &c.Phone, &c.CodeHash, &status, &failure, &vAtt, &sA,
		&c.NextAttemptAt, &waMsgID, &c.ClientIP, &c.CreatedAt, &c.SentAt, &c.ExpiresAt)
	if err != nil {
		return nil, err
	}
	c.Status = domain.OTPStatus(status)
	c.VerifyAttempts, c.SendAttempts = int(vAtt), int(sA)
	if failure != nil {
		c.FailureReason = *failure
	}
	if waMsgID != nil {
		c.WAMessageID = *waMsgID
	}
	return &c, nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *OTPRepo) Replace(ctx context.Context, c *domain.OTPChallenge) error {
	err := r.tx.WithTx(ctx, func(ctx context.Context) error {
		if _, err := r.db(ctx).Exec(ctx,
			`UPDATE otp_challenges SET status = 'expired' WHERE phone = $1 AND status IN ('queued', 'sending', 'sent')`, c.Phone); err != nil {
			return fmt.Errorf("postgres: expire previous otp: %w", err)
		}
		_, err := r.db(ctx).Exec(ctx, `INSERT INTO otp_challenges (`+otpColumns+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			c.ID, c.UserID, c.Phone, c.CodeHash, string(c.Status), nullable(c.FailureReason), c.VerifyAttempts, c.SendAttempts,
			c.NextAttemptAt, nullable(c.WAMessageID), c.ClientIP, c.CreatedAt, c.SentAt, c.ExpiresAt)
		if err != nil {
			if isUniqueViolation(err, "otp_one_active_per_phone") {
				// dua permintaan untuk nomor yang sama berbarengan: yang kalah diminta mengulang
				return domain.Conflict("OTP_REQUEST_IN_PROGRESS", "another code request for this number is in progress; retry shortly").WithRetryAfter(time.Second)
			}
			return fmt.Errorf("postgres: insert otp challenge: %w", err)
		}
		return nil
	})
	return err
}

func (r *OTPRepo) Get(ctx context.Context, id uuid.UUID) (*domain.OTPChallenge, error) {
	c, err := scanOTP(r.db(ctx).QueryRow(ctx, `SELECT `+otpColumns+` FROM otp_challenges WHERE id = $1`, id))
	return wrapOTP(c, err, id.String())
}

func (r *OTPRepo) LatestForPhone(ctx context.Context, phone string) (*domain.OTPChallenge, error) {
	c, err := scanOTP(r.db(ctx).QueryRow(ctx,
		`SELECT `+otpColumns+` FROM otp_challenges WHERE phone = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, phone))
	return wrapOTP(c, err, "phone")
}

func wrapOTP(c *domain.OTPChallenge, err error, what string) (*domain.OTPChallenge, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("otp challenge %s: %w", what, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get otp challenge: %w", err)
	}
	return c, nil
}

func (r *OTPRepo) count(ctx context.Context, sql string, args ...any) (int, error) {
	var n int
	if err := r.db(ctx).QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: count otp: %w", err)
	}
	return n, nil
}

func (r *OTPRepo) CountForPhoneSince(ctx context.Context, phone string, since time.Time) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM otp_challenges WHERE phone = $1 AND created_at >= $2`, phone, since)
}

func (r *OTPRepo) CountCreatedSince(ctx context.Context, since time.Time) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM otp_challenges WHERE created_at >= $1`, since)
}

func (r *OTPRepo) CountQueued(ctx context.Context) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM otp_challenges WHERE status = 'queued'`)
}

func (r *OTPRepo) ClaimNext(ctx context.Context, now time.Time) (*domain.OTPChallenge, error) {
	c, err := scanOTP(r.db(ctx).QueryRow(ctx, `
		WITH next AS (
			SELECT id FROM otp_challenges
			 WHERE status = 'queued' AND next_attempt_at <= $1
			 ORDER BY created_at, id
			 LIMIT 1
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE otp_challenges c
		   SET status = 'sending', send_attempts = c.send_attempts + 1, next_attempt_at = $1
		  FROM next
		 WHERE c.id = next.id
		RETURNING c.id, c.user_id, c.phone, c.code_hash, c.status, c.failure_reason, c.verify_attempts, c.send_attempts,
		          c.next_attempt_at, c.wa_message_id, c.client_ip, c.created_at, c.sent_at, c.expires_at`, now))
	return wrapOTP(c, err, "next")
}

func (r *OTPRepo) RequeueStuck(ctx context.Context, olderThan, now time.Time) (int, error) {
	tag, err := r.db(ctx).Exec(ctx,
		`UPDATE otp_challenges SET status = 'queued', next_attempt_at = $2 WHERE status = 'sending' AND next_attempt_at < $1`,
		olderThan, now)
	if err != nil {
		return 0, fmt.Errorf("postgres: requeue stuck otp: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// transition menjalankan UPDATE bersyarat; 0 baris berarti challenge sudah berpindah status
// (mis. diganti permintaan baru saat sedang dikirim) dan dilaporkan sebagai konflik.
func (r *OTPRepo) transition(ctx context.Context, sql string, args ...any) error {
	tag, err := r.db(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("postgres: update otp challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Conflict("OTP_STATE_CHANGED", "the code request is no longer in the expected state")
	}
	return nil
}

func (r *OTPRepo) MarkSent(ctx context.Context, id uuid.UUID, waMessageID string, sentAt, expiresAt time.Time) error {
	return r.transition(ctx, `UPDATE otp_challenges
		SET status = 'sent', wa_message_id = $2, sent_at = $3, expires_at = $4, failure_reason = NULL
		WHERE id = $1 AND status = 'sending'`, id, nullable(waMessageID), sentAt, expiresAt)
}

func (r *OTPRepo) Requeue(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time) error {
	return r.transition(ctx, `UPDATE otp_challenges SET status = 'queued', next_attempt_at = $2 WHERE id = $1 AND status = 'sending'`, id, nextAttemptAt)
}

func (r *OTPRepo) MarkFailed(ctx context.Context, id uuid.UUID, reason string) error {
	return r.transition(ctx, `UPDATE otp_challenges SET status = 'failed', failure_reason = $2 WHERE id = $1 AND status = 'sending'`, id, reason)
}

func (r *OTPRepo) IncrementVerifyAttempts(ctx context.Context, id uuid.UUID) (int, error) {
	var n int16
	err := r.db(ctx).QueryRow(ctx,
		`UPDATE otp_challenges SET verify_attempts = verify_attempts + 1 WHERE id = $1 RETURNING verify_attempts`, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("otp challenge %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("postgres: increment verify attempts: %w", err)
	}
	return int(n), nil
}

func (r *OTPRepo) Consume(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE otp_challenges SET status = 'consumed' WHERE id = $1 AND status = 'sent'`, id)
	if err != nil {
		return false, fmt.Errorf("postgres: consume otp: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *OTPRepo) Expire(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db(ctx).Exec(ctx,
		`UPDATE otp_challenges SET status = 'expired' WHERE id = $1 AND status IN ('queued', 'sending', 'sent')`, id); err != nil {
		return fmt.Errorf("postgres: expire otp: %w", err)
	}
	return nil
}

func (r *OTPRepo) DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	tag, err := r.db(ctx).Exec(ctx, `
		DELETE FROM otp_challenges WHERE id IN (
			SELECT id FROM otp_challenges WHERE created_at < $1 AND status NOT IN ('queued', 'sending')
			 ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED
		)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete otp challenges: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
