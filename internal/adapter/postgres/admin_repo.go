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

// AdminSubmissionRepo membaca submission untuk dashboard admin (spec-labels-admin §5.4). Memuat data
// pribadi annotator: hanya dipakai rute admin.
type AdminSubmissionRepo struct{ base }

var _ usecase.AdminSubmissionRepo = (*AdminSubmissionRepo)(nil)

func NewAdminSubmissionRepo(pool *pgxpool.Pool) *AdminSubmissionRepo {
	return &AdminSubmissionRepo{base{pool}}
}

// likeEscape meloloskan karakter khusus LIKE agar input admin dicari apa adanya.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// sqlArgs menyusun klausa WHERE dengan placeholder bernomor.
type sqlArgs struct{ args []any }

func (a *sqlArgs) add(v any) string {
	a.args = append(a.args, v)
	return fmt.Sprintf("$%d", len(a.args))
}

func (r *AdminSubmissionRepo) List(ctx context.Context, q usecase.AdminListQuery) ([]usecase.AdminSubmissionRow, error) {
	var a sqlArgs
	where := []string{
		`s.deleted_at IS NULL`,
		// hanya submission yang masih punya foto terlihat (retensi/hapus menandai foto deleted lebih dulu)
		`EXISTS (SELECT 1 FROM photos ep WHERE ep.submission_id = s.id AND ep.status = 'attached')`,
	}
	if q.From != nil {
		where = append(where, `s.created_at >= `+a.add(*q.From))
	}
	if q.To != nil {
		where = append(where, `s.created_at < `+a.add(*q.To))
	}
	if q.UserID != nil {
		where = append(where, `s.owner_id = `+a.add(*q.UserID))
	}
	if q.LabelID != nil {
		where = append(where, `EXISTS (SELECT 1 FROM photos fp JOIN annotations fa ON fa.photo_id = fp.id
			WHERE fp.submission_id = s.id AND fp.status = 'attached' AND fa.label_id = `+a.add(*q.LabelID)+`)`)
	}
	if q.Unlabeled {
		where = append(where, `EXISTS (SELECT 1 FROM photos fp JOIN annotations fa ON fa.photo_id = fp.id
			WHERE fp.submission_id = s.id AND fp.status = 'attached' AND fa.label_id IS NULL)`)
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
	order, cmp := "DESC", "<"
	if q.Oldest {
		order, cmp = "ASC", ">"
	}
	if q.After != nil {
		where = append(where, `(s.created_at, s.id) `+cmp+` (`+a.add(q.After.CreatedAt)+`, `+a.add(q.After.ID)+`::uuid)`)
	}
	sql := `SELECT s.id, s.owner_id, s.version, s.created_at, s.updated_at, u.id, u.name, u.email, u.phone
		  FROM submissions s LEFT JOIN users u ON u.id = s.owner_id
		 WHERE ` + strings.Join(where, " AND ") + `
		 ORDER BY s.created_at ` + order + `, s.id ` + order + `
		 LIMIT ` + a.add(q.Limit)
	rows, err := r.db(ctx).Query(ctx, sql, a.args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: admin list submissions: %w", err)
	}
	defer rows.Close()
	out := []usecase.AdminSubmissionRow{}
	index := map[uuid.UUID]int{}
	var ids []uuid.UUID
	for rows.Next() {
		var (
			row                usecase.AdminSubmissionRow
			uid                *uuid.UUID
			name, email, phone *string
		)
		s := &row.Submission
		if err := rows.Scan(&s.ID, &s.OwnerID, &s.Version, &s.CreatedAt, &s.UpdatedAt, &uid, &name, &email, &phone); err != nil {
			return nil, fmt.Errorf("postgres: scan admin submission: %w", err)
		}
		if uid != nil {
			row.Annotator = &usecase.Annotator{ID: *uid, Name: *name, Email: *email, Phone: *phone}
		}
		row.Labels = []usecase.LabelCount{}
		index[s.ID] = len(out)
		ids = append(ids, s.ID)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: admin submissions rows: %w", err)
	}
	if len(ids) == 0 {
		return out, nil
	}
	if err := r.fillCounts(ctx, ids, index, out); err != nil {
		return nil, err
	}
	if err := r.fillCovers(ctx, ids, index, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fillCounts mengisi jumlah foto, kotak per label, dan kotak teks lama untuk satu halaman.
func (r *AdminSubmissionRepo) fillCounts(ctx context.Context, ids []uuid.UUID, index map[uuid.UUID]int, out []usecase.AdminSubmissionRow) error {
	rows, err := r.db(ctx).Query(ctx, `
		SELECT p.submission_id, count(DISTINCT p.id) FROM photos p
		 WHERE p.submission_id = ANY($1) AND p.status = 'attached' GROUP BY p.submission_id`, ids)
	if err != nil {
		return fmt.Errorf("postgres: admin photo counts: %w", err)
	}
	for rows.Next() {
		var (
			sid uuid.UUID
			n   int
		)
		if err := rows.Scan(&sid, &n); err != nil {
			rows.Close()
			return fmt.Errorf("postgres: scan photo count: %w", err)
		}
		out[index[sid]].PhotoCount = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = r.db(ctx).Query(ctx, `
		SELECT p.submission_id, a.label_id, l.name, l.color, count(*)
		  FROM annotations a JOIN photos p ON p.id = a.photo_id LEFT JOIN labels l ON l.id = a.label_id
		 WHERE p.submission_id = ANY($1) AND p.status = 'attached'
		 GROUP BY p.submission_id, a.label_id, l.name, l.color
		 ORDER BY p.submission_id, count(*) DESC, a.label_id`, ids)
	if err != nil {
		return fmt.Errorf("postgres: admin label counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			sid         uuid.UUID
			lid         *int
			name, color *string
			n           int
		)
		if err := rows.Scan(&sid, &lid, &name, &color, &n); err != nil {
			return fmt.Errorf("postgres: scan label count: %w", err)
		}
		row := &out[index[sid]]
		row.AnnotationCount += n
		if lid == nil {
			row.LegacyTextCount += n
			continue
		}
		row.Labels = append(row.Labels, usecase.LabelCount{ID: *lid, Name: *name, Color: *color, Count: n})
	}
	return rows.Err()
}

// fillCovers mengisi foto sampul (foto attached pertama menurut position) beserta kotaknya.
func (r *AdminSubmissionRepo) fillCovers(ctx context.Context, ids []uuid.UUID, index map[uuid.UUID]int, out []usecase.AdminSubmissionRow) error {
	rows, err := r.db(ctx).Query(ctx, `
		SELECT c.submission_id, c.id, c.position, c.content_type, c.size_bytes, c.width, c.height,
		       a.id, a.label_id, a.text, a.x, a.y, a.w, a.h, l.name, l.color, l.archived_at
		  FROM (SELECT DISTINCT ON (submission_id) submission_id, id, position, content_type, size_bytes, width, height
		          FROM photos WHERE submission_id = ANY($1) AND status = 'attached'
		         ORDER BY submission_id, position, id) c
		  LEFT JOIN annotations a ON a.photo_id = c.id
		  LEFT JOIN labels l ON l.id = a.label_id
		 ORDER BY c.submission_id, a.id`, ids)
	if err != nil {
		return fmt.Errorf("postgres: admin covers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			sid               uuid.UUID
			ph                domain.Photo
			pos               int16
			aid               *int64
			lid               *int
			text, name, color *string
			x, y, w, h        *float64
			archivedAt        *time.Time
		)
		if err := rows.Scan(&sid, &ph.ID, &pos, &ph.ContentType, &ph.SizeBytes, &ph.Width, &ph.Height,
			&aid, &lid, &text, &x, &y, &w, &h, &name, &color, &archivedAt); err != nil {
			return fmt.Errorf("postgres: scan cover: %w", err)
		}
		row := &out[index[sid]]
		if row.Cover == nil {
			ph.Position, ph.SubmissionID, ph.Status = int(pos), &sid, domain.PhotoAttached
			row.Cover = &domain.PhotoWithAnnotations{Photo: ph, Annotations: []domain.Annotation{}}
		}
		if aid != nil {
			row.Cover.Annotations = append(row.Cover.Annotations, annotationFrom(lid, text, name, color, archivedAt, *x, *y, *w, *h))
		}
	}
	return rows.Err()
}

func (r *AdminSubmissionRepo) Annotator(ctx context.Context, userID uuid.UUID) (*usecase.Annotator, error) {
	var a usecase.Annotator
	err := r.db(ctx).QueryRow(ctx, `SELECT id, name, email, phone FROM users WHERE id = $1`, userID).
		Scan(&a.ID, &a.Name, &a.Email, &a.Phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: annotator: %w", err)
	}
	return &a, nil
}
