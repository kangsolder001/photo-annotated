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

type GroupRepo struct{ base }

var _ usecase.GroupRepo = (*GroupRepo)(nil)

func NewGroupRepo(pool *pgxpool.Pool) *GroupRepo { return &GroupRepo{base{pool}} }

const groupColumns = `g.id, g.name, g.archived_at, g.created_at, g.updated_at`

// groupStudents menghitung akun yang memakai kelompok g.
const groupStudents = `0`

func scanGroup(row pgx.Row, extra ...any) (*domain.Group, error) {
	var g domain.Group
	if err := row.Scan(append([]any{&g.ID, &g.Name, &g.ArchivedAt, &g.CreatedAt, &g.UpdatedAt}, extra...)...); err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *GroupRepo) ListActive(ctx context.Context) ([]domain.Group, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+groupColumns+` FROM groups g WHERE g.archived_at IS NULL ORDER BY lower(g.name), g.id`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list groups: %w", err)
	}
	defer rows.Close()
	out := []domain.Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan group: %w", err)
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (r *GroupRepo) ListAll(ctx context.Context) ([]usecase.GroupUsage, error) {
	rows, err := r.db(ctx).Query(ctx, `SELECT `+groupColumns+`, `+groupStudents+` FROM groups g
		ORDER BY (g.archived_at IS NOT NULL), lower(g.name), g.id`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list groups: %w", err)
	}
	defer rows.Close()
	out := []usecase.GroupUsage{}
	for rows.Next() {
		var n int
		g, err := scanGroup(rows, &n)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan group: %w", err)
		}
		out = append(out, usecase.GroupUsage{Group: *g, StudentCount: n})
	}
	return out, rows.Err()
}

func errGroupNameTaken() error {
	return domain.Conflict("GROUP_NAME_TAKEN", "a group with this name already exists (names are case-insensitive, archived groups included)")
}

func (r *GroupRepo) Create(ctx context.Context, g *domain.Group) error {
	err := r.db(ctx).QueryRow(ctx, `INSERT INTO groups (name, created_at, updated_at) VALUES ($1, $2, $3) RETURNING id`,
		g.Name, g.CreatedAt, g.UpdatedAt).Scan(&g.ID)
	if isUniqueViolation(err, "groups_name_lower") {
		return errGroupNameTaken()
	}
	if err != nil {
		return fmt.Errorf("postgres: insert group: %w", err)
	}
	return nil
}

func (r *GroupRepo) Update(ctx context.Context, id int, name string, archived bool, now time.Time) (*usecase.GroupUsage, error) {
	var n int
	g, err := scanGroup(r.db(ctx).QueryRow(ctx, `
		UPDATE groups g SET name = $2, updated_at = $4,
		       archived_at = CASE WHEN $3 THEN COALESCE(g.archived_at, $4) ELSE NULL END
		 WHERE g.id = $1
		RETURNING `+groupColumns+`, `+groupStudents, id, name, archived, now), &n)
	if isUniqueViolation(err, "groups_name_lower") {
		return nil, errGroupNameTaken()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("group %d: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: update group: %w", err)
	}
	return &usecase.GroupUsage{Group: *g, StudentCount: n}, nil
}

func (r *GroupRepo) Delete(ctx context.Context, id int) (*domain.Group, error) {
	g, err := scanGroup(r.db(ctx).QueryRow(ctx, `DELETE FROM groups g WHERE g.id = $1 RETURNING `+groupColumns, id))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // FK ON DELETE RESTRICT dari users.group_id
		return nil, domain.Conflict("GROUP_IN_USE", "the group is used by accounts; archive it instead")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("group %d: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: delete group: %w", err)
	}
	return g, nil
}
