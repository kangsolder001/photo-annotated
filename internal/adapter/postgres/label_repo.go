package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

type LabelRepo struct{ base }

var _ usecase.LabelRepo = (*LabelRepo)(nil)

func NewLabelRepo(pool *pgxpool.Pool) *LabelRepo { return &LabelRepo{base{pool}} }

// labelWriteLock adalah kunci advisory (per transaksi) untuk semua penulisan label.
const labelWriteLock int64 = 0x6c6162656c73 // "labels"

const labelColumns = `id, name, description, color, sort_order, archived_at, created_at, updated_at`

func scanLabel(row pgx.Row, extra ...any) (*domain.Label, error) {
	var (
		l    domain.Label
		desc *string
	)
	dst := append([]any{&l.ID, &l.Name, &desc, &l.Color, &l.SortOrder, &l.ArchivedAt, &l.CreatedAt, &l.UpdatedAt}, extra...)
	if err := row.Scan(dst...); err != nil {
		return nil, err
	}
	if desc != nil {
		l.Description = *desc
	}
	return &l, nil
}

func (r *LabelRepo) LockForWrite(ctx context.Context) error {
	if _, err := r.db(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, labelWriteLock); err != nil {
		return fmt.Errorf("postgres: lock labels: %w", err)
	}
	return nil
}

func (r *LabelRepo) ListActive(ctx context.Context) ([]domain.Label, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+labelColumns+` FROM labels WHERE archived_at IS NULL ORDER BY sort_order, lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list labels: %w", err)
	}
	defer rows.Close()
	out := []domain.Label{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan label: %w", err)
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r *LabelRepo) ListAll(ctx context.Context) ([]usecase.LabelUsage, error) {
	rows, err := r.db(ctx).Query(ctx, `
		SELECT l.id, l.name, l.description, l.color, l.sort_order, l.archived_at, l.created_at, l.updated_at,
		       COALESCE(u.n, 0)
		  FROM labels l
		  LEFT JOIN (
			SELECT a.label_id, count(*) AS n
			  FROM annotations a JOIN photos p ON p.id = a.photo_id
			 WHERE a.label_id IS NOT NULL AND p.status = 'attached'
			 GROUP BY a.label_id
		  ) u ON u.label_id = l.id
		 ORDER BY (l.archived_at IS NOT NULL), l.sort_order, lower(l.name), l.id`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list labels: %w", err)
	}
	defer rows.Close()
	out := []usecase.LabelUsage{}
	for rows.Next() {
		var n int
		l, err := scanLabel(rows, &n)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan label: %w", err)
		}
		out = append(out, usecase.LabelUsage{Label: *l, UsageCount: n})
	}
	return out, rows.Err()
}

// ForShare membaca label dengan kunci FOR SHARE (dipakai submit/edit di dalam transaksi): label yang
// sedang diarsipkan/dihapus berbarengan menunggu transaksi ini selesai, dan sebaliknya.
func (r *LabelRepo) ForShare(ctx context.Context, ids []int) (map[int]domain.Label, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+labelColumns+` FROM labels WHERE id = ANY($1) ORDER BY id FOR SHARE`, ids)
	if err != nil {
		return nil, fmt.Errorf("postgres: lock labels for share: %w", err)
	}
	defer rows.Close()
	out := make(map[int]domain.Label, len(ids))
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan label: %w", err)
		}
		out[l.ID] = *l
	}
	return out, rows.Err()
}

func (r *LabelRepo) Get(ctx context.Context, id int) (*domain.Label, error) {
	l, err := scanLabel(r.db(ctx).QueryRow(ctx, `SELECT `+labelColumns+` FROM labels WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("label %d: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get label: %w", err)
	}
	return l, nil
}

func (r *LabelRepo) count(ctx context.Context, sql string, args ...any) (int, error) {
	var n int
	if err := r.db(ctx).QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: count labels: %w", err)
	}
	return n, nil
}

func (r *LabelRepo) CountActive(ctx context.Context) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM labels WHERE archived_at IS NULL`)
}

func (r *LabelRepo) CountAll(ctx context.Context) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM labels`)
}

func (r *LabelRepo) MaxActiveOrder(ctx context.Context) (int, error) {
	return r.count(ctx, `SELECT COALESCE(max(sort_order), -1) FROM labels WHERE archived_at IS NULL`)
}

func (r *LabelRepo) UsageAttached(ctx context.Context, id int) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM annotations a JOIN photos p ON p.id = a.photo_id
		WHERE a.label_id = $1 AND p.status = 'attached'`, id)
}

func (r *LabelRepo) UsageAll(ctx context.Context, id int) (int, error) {
	return r.count(ctx, `SELECT count(*) FROM annotations WHERE label_id = $1`, id)
}

func errNameTaken() error {
	return domain.Conflict("LABEL_NAME_TAKEN", "a label with this name already exists (names are case-insensitive, archived labels included)")
}

func (r *LabelRepo) Create(ctx context.Context, l *domain.Label) error {
	err := r.db(ctx).QueryRow(ctx, `
		INSERT INTO labels (name, description, color, sort_order, created_at, updated_at)
		VALUES ($1, $2, $3, (SELECT COALESCE(max(sort_order), -1) + 1 FROM labels WHERE archived_at IS NULL), $4, $5)
		RETURNING id, sort_order`, l.Name, nullable(l.Description), l.Color, l.CreatedAt, l.UpdatedAt).Scan(&l.ID, &l.SortOrder)
	if isUniqueViolation(err, "labels_name_lower") {
		return errNameTaken()
	}
	if err != nil {
		return fmt.Errorf("postgres: insert label: %w", err)
	}
	return nil
}

func (r *LabelRepo) Update(ctx context.Context, l *domain.Label) error {
	tag, err := r.db(ctx).Exec(ctx, `
		UPDATE labels SET name = $2, description = $3, color = $4, archived_at = $5, sort_order = $6, updated_at = $7
		 WHERE id = $1`, l.ID, l.Name, nullable(l.Description), l.Color, l.ArchivedAt, l.SortOrder, l.UpdatedAt)
	if isUniqueViolation(err, "labels_name_lower") {
		return errNameTaken()
	}
	if err != nil {
		return fmt.Errorf("postgres: update label: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("label %d: %w", l.ID, domain.ErrNotFound)
	}
	return nil
}

func (r *LabelRepo) SetOrder(ctx context.Context, ids []int, now time.Time) error {
	if _, err := r.db(ctx).Exec(ctx, `
		UPDATE labels l SET sort_order = o.ord - 1, updated_at = $2
		  FROM unnest($1::int[]) WITH ORDINALITY AS o(id, ord)
		 WHERE l.id = o.id`, ids, now); err != nil {
		return fmt.Errorf("postgres: reorder labels: %w", err)
	}
	return nil
}

func (r *LabelRepo) Delete(ctx context.Context, id int) error {
	tag, err := r.db(ctx).Exec(ctx, `DELETE FROM labels WHERE id = $1`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // FK ON DELETE RESTRICT dari annotations
		return domain.Conflict("LABEL_IN_USE", "the label is used by annotations; archive it instead")
	}
	if err != nil {
		return fmt.Errorf("postgres: delete label: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("label %d: %w", id, domain.ErrNotFound)
	}
	return nil
}
