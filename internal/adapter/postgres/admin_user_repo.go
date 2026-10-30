package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// AdminUserRepo membaca dan memblokir annotator (spec-labels-admin §5.8).
type AdminUserRepo struct{ base }

var _ usecase.AdminUserRepo = (*AdminUserRepo)(nil)

func NewAdminUserRepo(pool *pgxpool.Pool) *AdminUserRepo { return &AdminUserRepo{base{pool}} }

func (r *AdminUserRepo) ListUsers(ctx context.Context, q usecase.AdminUserQuery) ([]usecase.AdminUserRow, error) {
	var a sqlArgs
	where := []string{`u.status = 'active'`}
	if q.Blocked != nil {
		if *q.Blocked {
			where = append(where, `u.blocked_at IS NOT NULL`)
		} else {
			where = append(where, `u.blocked_at IS NULL`)
		}
	}
	var who []string
	if q.NameQ != "" {
		who = append(who, `u.name ILIKE `+a.add("%"+likeEscape(q.NameQ)+"%"))
	}
	if q.PhoneQ != "" {
		who = append(who, `u.phone LIKE `+a.add(likeEscape(q.PhoneQ)+"%"))
	}
	if len(who) > 0 {
		where = append(where, "("+strings.Join(who, " OR ")+")")
	}
	if q.After != nil {
		where = append(where, `(u.created_at, u.id) < (`+a.add(q.After.CreatedAt)+`, `+a.add(q.After.ID)+`::uuid)`)
	}
	rows, err := r.db(ctx).Query(ctx, userSelect+`
		 WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY u.created_at DESC, u.id DESC LIMIT `+a.add(q.Limit), a.args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list users: %w", err)
	}
	defer rows.Close()
	out := []usecase.AdminUserRow{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan user: %w", err)
		}
		out = append(out, usecase.AdminUserRow{User: *u})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, r.fillStats(ctx, out)
}

// prefixed menambahkan alias tabel ke daftar kolom "a, b, c".
func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// fillStats menghitung statistik per pengguna dari submission yang belum dihapus (satu query).
func (r *AdminUserRepo) fillStats(ctx context.Context, rows []usecase.AdminUserRow) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i, row := range rows {
		ids[i], index[row.User.ID] = row.User.ID, i
	}
	res, err := r.db(ctx).Query(ctx, `
		SELECT s.owner_id, count(*),
		       count(*) FILTER (WHERE s.review_status = 'accepted'),
		       count(*) FILTER (WHERE s.review_status = 'rejected'),
		       count(*) FILTER (WHERE s.review_status = 'pending'),
		       max(s.created_at),
		       COALESCE(sum((SELECT count(*) FROM annotations a JOIN photos p ON p.id = a.photo_id
		                      WHERE p.submission_id = s.id AND p.status = 'attached')), 0)
		  FROM submissions s
		 WHERE s.owner_id = ANY($1) AND s.deleted_at IS NULL
		 GROUP BY s.owner_id`, ids)
	if err != nil {
		return fmt.Errorf("postgres: user stats: %w", err)
	}
	defer res.Close()
	for res.Next() {
		var (
			uid uuid.UUID
			st  usecase.UserStats
		)
		if err := res.Scan(&uid, &st.Submissions, &st.Accepted, &st.Rejected, &st.Pending, &st.LastSubmissionAt, &st.Annotations); err != nil {
			return fmt.Errorf("postgres: scan user stats: %w", err)
		}
		rows[index[uid]].Stats = st
	}
	return res.Err()
}

func (r *AdminUserRepo) GetUser(ctx context.Context, id uuid.UUID) (*usecase.AdminUserRow, error) {
	u, err := scanUser(r.db(ctx).QueryRow(ctx, userSelect+` WHERE u.id = $1 AND u.status = 'active'`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get user: %w", err)
	}
	rows := []usecase.AdminUserRow{{User: *u}}
	if err := r.fillStats(ctx, rows); err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// activeExists membedakan "tidak berubah" dari "tidak ada".
func (r *AdminUserRepo) activeExists(ctx context.Context, id uuid.UUID) error {
	var ok bool
	if err := r.db(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND status = 'active')`, id).Scan(&ok); err != nil {
		return fmt.Errorf("postgres: user exists: %w", err)
	}
	if !ok {
		return fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *AdminUserRepo) Block(ctx context.Context, id uuid.UUID, reason string, now time.Time) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE users SET blocked_at = $2, blocked_reason = $3
		WHERE id = $1 AND status = 'active' AND blocked_at IS NULL`, id, now, reason)
	if err != nil {
		return false, fmt.Errorf("postgres: block user: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	return false, r.activeExists(ctx, id)
}

func (r *AdminUserRepo) LockForDelete(ctx context.Context, id uuid.UUID) error {
	var one int
	err := r.db(ctx).QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 AND status = 'active' FOR UPDATE`, id).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("postgres: lock user: %w", err)
	}
	return nil
}

func (r *AdminUserRepo) DeleteUser(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db(ctx).Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *AdminUserRepo) SetGroup(ctx context.Context, id uuid.UUID, groupID int) error {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE users SET group_id = $2
		WHERE id = $1 AND status = 'active'
		  AND EXISTS (SELECT 1 FROM groups WHERE id = $2 AND archived_at IS NULL)`, id, groupID)
	if err != nil {
		return fmt.Errorf("postgres: set user group: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	if err := r.activeExists(ctx, id); err != nil {
		return err
	}
	return usecase.ErrInvalidGroup
}

func (r *AdminUserRepo) Unblock(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db(ctx).Exec(ctx, `UPDATE users SET blocked_at = NULL, blocked_reason = NULL
		WHERE id = $1 AND status = 'active' AND blocked_at IS NOT NULL`, id)
	if err != nil {
		return false, fmt.Errorf("postgres: unblock user: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	return false, r.activeExists(ctx, id)
}
