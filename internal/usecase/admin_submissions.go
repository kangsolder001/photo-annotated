package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// AdminSubmissionsConfig: URL lihat foto dan perhitungan batas retensi untuk dashboard.
type AdminSubmissionsConfig struct {
	PublicBaseURL string
	ViewURLTTL    time.Duration
	EditWindow    time.Duration
	RetentionDays int
}

// AdminSubmissions menyajikan hasil anotasi untuk dashboard admin (spec-labels-admin §5.4): daftar
// dengan filter dan foto sampul, serta detail lengkap beserta identitas annotator.
type AdminSubmissions struct {
	repo   AdminSubmissionRepo
	subs   SubmissionRepo
	signer URLSigner
	clock  Clock
	cfg    AdminSubmissionsConfig
}

func NewAdminSubmissions(repo AdminSubmissionRepo, subs SubmissionRepo, signer URLSigner, clock Clock, cfg AdminSubmissionsConfig) *AdminSubmissions {
	if cfg.ViewURLTTL <= 0 || cfg.EditWindow <= 0 || cfg.RetentionDays < 1 {
		panic("usecase: AdminSubmissionsConfig tidak valid")
	}
	return &AdminSubmissions{repo: repo, subs: subs, signer: signer, clock: clock, cfg: cfg}
}

const (
	AdminListDefaultLimit = 20
	AdminListMaxLimit     = 50
)

// AdminListItem adalah baris daftar beserta URL sampul bertanda tangan.
type AdminListItem struct {
	AdminSubmissionRow
	RetentionDeletesAt *time.Time // nil bila tidak ikut retensi (diterima, §5.7)
	CoverURL           string
}

// AdminListResult: Next nil bila tidak ada halaman berikutnya.
type AdminListResult struct {
	Items []AdminListItem
	Next  *AdminCursor
}

// retentionDeletesAt: kapan submission ini akan terhapus retensi.
func (u *AdminSubmissions) retentionDeletesAt(s *domain.Submission) *time.Time {
	t := s.CreatedAt.Add(time.Duration(u.cfg.RetentionDays) * 24 * time.Hour)
	return &t
}

// List mengembalikan satu halaman submission. Limit di luar 1..50 → INVALID_QUERY.
func (u *AdminSubmissions) List(ctx context.Context, q AdminListQuery) (*AdminListResult, error) {
	if q.Limit == 0 {
		q.Limit = AdminListDefaultLimit
	}
	if q.Limit < 1 || q.Limit > AdminListMaxLimit {
		return nil, domain.Invalid("INVALID_QUERY", "limit must be between 1 and %d", AdminListMaxLimit)
	}
	want := q.Limit
	q.Limit++ // satu baris tambahan menandai ada halaman berikutnya
	rows, err := u.repo.List(ctx, q)
	if err != nil {
		return nil, err
	}
	res := &AdminListResult{Items: make([]AdminListItem, 0, min(len(rows), want))}
	if len(rows) > want {
		rows = rows[:want]
		last := rows[len(rows)-1].Submission
		res.Next = &AdminCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	exp := u.clock.Now().Add(u.cfg.ViewURLTTL)
	for _, r := range rows {
		it := AdminListItem{AdminSubmissionRow: r, RetentionDeletesAt: u.retentionDeletesAt(&r.Submission)}
		if r.Cover != nil {
			it.CoverURL = viewURL(u.cfg.PublicBaseURL, u.signer, r.Cover.Photo.ID, exp)
		}
		res.Items = append(res.Items, it)
	}
	return res, nil
}

// AdminDetail adalah submission lengkap untuk dashboard: seperti GET /submissions/{id} ditambah
// annotator, batas retensi, serta URL bertanda tangan per foto.
type AdminDetail struct {
	View               SubmissionView
	Annotator          *Annotator
	RetentionDeletesAt *time.Time
	PhotoURLs          map[uuid.UUID]string
	URLExpiresAt       time.Time
}

// Detail mengembalikan submission lengkap; id tak ada → domain.ErrNotFound.
func (u *AdminSubmissions) Detail(ctx context.Context, id uuid.UUID) (*AdminDetail, error) {
	d, err := u.subs.GetDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	now := u.clock.Now()
	until := d.Submission.CreatedAt.Add(u.cfg.EditWindow)
	out := &AdminDetail{
		View:               SubmissionView{Detail: *d, EditableUntil: until, Editable: now.Before(until)},
		RetentionDeletesAt: u.retentionDeletesAt(&d.Submission),
		PhotoURLs:          make(map[uuid.UUID]string, len(d.Photos)),
		URLExpiresAt:       now.Add(u.cfg.ViewURLTTL),
	}
	if d.Submission.OwnerID != nil {
		if out.Annotator, err = u.repo.Annotator(ctx, *d.Submission.OwnerID); err != nil {
			return nil, err
		}
	}
	for _, p := range d.Photos {
		out.PhotoURLs[p.Photo.ID] = viewURL(u.cfg.PublicBaseURL, u.signer, p.Photo.ID, out.URLExpiresAt)
	}
	return out, nil
}
