package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/usecase"
)

type txKey struct{}

// dbtx adalah irisan metode yang dipakai repository; dipenuhi *pgxpool.Pool maupun pgx.Tx.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// base memberi repository akses ke transaksi aktif (bila ctx berasal dari TxManager.WithTx)
// atau ke pool bila tidak ada transaksi.
type base struct{ pool *pgxpool.Pool }

func (b base) db(ctx context.Context) dbtx {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return b.pool
}

// TxManager menjalankan fungsi dalam satu transaksi. Repository yang dipanggil dengan ctx
// dari fungsi itu otomatis ikut transaksi.
type TxManager struct{ pool *pgxpool.Pool }

var _ usecase.TxManager = (*TxManager)(nil)

func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// WithTx: fn error atau panic → rollback (panic diteruskan); selain itu commit. Pemanggilan
// bersarang bergabung ke transaksi luar (tanpa savepoint): kegagalan di dalam harus
// dikembalikan sebagai error agar transaksi luar ikut dibatalkan.
func (m *TxManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	done := false
	defer func() {
		if done {
			return
		}
		// panic: batalkan, lalu teruskan panic ke pemanggil
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		done = true
		if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("postgres: rollback: %w", rbErr))
		}
		return err
	}
	done = true
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	return nil
}

func isUniqueViolation(err error, constraintContains string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraintContains == "" || contains(pgErr.ConstraintName, constraintContains)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
