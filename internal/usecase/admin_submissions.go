package usecase

import (
	"context"
	"errors"
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
	if cfg.ViewURLTTL <= 0 || cfg.EditWindow <= 0 || cfg.RetentionDays < 0 {
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

// retentionDeletesAt: kapan submission ini akan terhapus retensi; nil bila diterima (tidak ikut retensi).
func (u *AdminSubmissions) retentionDeletesAt(s *domain.Submission) *time.Time {
	return RetentionDeletesAt(s, u.cfg.RetentionDays)
}

// RetentionDeletesAt = created_at + retention.days, atau nil untuk submission accepted (spec §5.7) dan
// bila retensi mati (days = 0, spec-pendataan §8).
func RetentionDeletesAt(s *domain.Submission, days int) *time.Time {
	if days == 0 || s.Review.Status == domain.ReviewAccepted {
		return nil
	}
	t := s.CreatedAt.AddDate(0, 0, days)
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

// ReviewResult adalah hasil satu review: status baru dan batas retensi yang berlaku sesudahnya.
type ReviewResult struct {
	Review             domain.Review
	RetentionDeletesAt *time.Time
}

func parseReview(status, reason string, now time.Time) (domain.Review, error) {
	st, err := domain.ParseReviewStatus(status)
	if err != nil {
		return domain.Review{}, err
	}
	return domain.NewReview(st, reason, now)
}

// Review menerima/menolak/membatalkan review satu submission (spec-labels-admin §5.7). version = versi
// yang dilihat admin; bila pengguna sudah mengedit sejak itu → VERSION_CONFLICT, sehingga admin tidak
// pernah menerima isi yang belum ia lihat.
func (u *AdminSubmissions) Review(ctx context.Context, id uuid.UUID, version int, status, reason string) (*ReviewResult, error) {
	if version < 1 {
		return nil, domain.Invalid("INVALID_VERSION", "version must be the version you reviewed (>= 1)")
	}
	rv, err := parseReview(status, reason, u.clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, err
	}
	if err := u.subs.SetReview(ctx, id, version, rv); err != nil {
		return nil, err
	}
	s, err := u.subs.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ReviewResult{Review: s.Review, RetentionDeletesAt: u.retentionDeletesAt(s)}, nil
}

// ReviewItem adalah satu submission pada review massal.
type ReviewItem struct {
	ID      uuid.UUID
	Version int
}

// Hasil per item review massal.
const (
	ReviewResultOK              = "ok"
	ReviewResultVersionConflict = "version_conflict"
	ReviewResultNotFound        = "not_found"
)

// ReviewItemResult adalah hasil satu item review massal.
type ReviewItemResult struct {
	ID     uuid.UUID
	Result string
}

// ReviewMany menerapkan satu keputusan (dan satu alasan) ke 1–50 submission; diproses per item.
func (u *AdminSubmissions) ReviewMany(ctx context.Context, items []ReviewItem, status, reason string) ([]ReviewItemResult, error) {
	if len(items) < 1 || len(items) > MaxBulkDelete {
		return nil, domain.Invalid("INVALID_ITEMS", "items must contain between 1 and %d submissions", MaxBulkDelete)
	}
	seen := make(map[uuid.UUID]bool, len(items))
	for _, it := range items {
		if it.ID == uuid.Nil || it.Version < 1 || seen[it.ID] {
			return nil, domain.Invalid("INVALID_ITEMS", "items must be distinct submissions, each with its version (>= 1)")
		}
		seen[it.ID] = true
	}
	rv, err := parseReview(status, reason, u.clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, err
	}
	out := make([]ReviewItemResult, 0, len(items))
	for _, it := range items {
		err := u.subs.SetReview(ctx, it.ID, it.Version, rv)
		res := ReviewItemResult{ID: it.ID, Result: ReviewResultOK}
		switch {
		case err == nil:
		case errors.Is(err, domain.ErrNotFound):
			res.Result = ReviewResultNotFound
		case isCode(err, "VERSION_CONFLICT"):
			res.Result = ReviewResultVersionConflict
		default:
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

func isCode(err error, code string) bool {
	de, ok := domain.AsError(err)
	return ok && de.Code == code
}
