package handler

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// AdminSubmissionReader dipenuhi oleh *usecase.AdminSubmissions.
type AdminSubmissionReader interface {
	List(ctx context.Context, q usecase.AdminListQuery) (*usecase.AdminListResult, error)
	Detail(ctx context.Context, id uuid.UUID) (*usecase.AdminDetail, error)
}

// AdminSubmissionHandler menangani hasil anotasi di dashboard (spec-labels-admin §5.4). Respons memuat
// data pribadi annotator: selalu Cache-Control no-store.
type AdminSubmissionHandler struct {
	reader AdminSubmissionReader
}

func NewAdminSubmissionHandler(r AdminSubmissionReader) *AdminSubmissionHandler {
	return &AdminSubmissionHandler{reader: r}
}

func invalidQuery(format string, a ...any) error {
	return domain.Invalid("INVALID_QUERY", format, a...)
}

// adminCursor adalah isi cursor opak (base64url JSON). Arah urutan ikut disimpan agar cursor dari
// urutan lain ditolak, bukan menghasilkan halaman yang salah.
type adminCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
	O  bool      `json:"o"`
}

func encodeCursor(c *usecase.AdminCursor, oldest bool) *string {
	if c == nil {
		return nil
	}
	b, _ := json.Marshal(adminCursor{T: c.CreatedAt, ID: c.ID, O: oldest})
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}

func decodeCursor(s string, oldest bool) (*usecase.AdminCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	var c adminCursor
	if err != nil || len(raw) > 512 || json.Unmarshal(raw, &c) != nil || c.ID == uuid.Nil || c.T.IsZero() || c.O != oldest {
		return nil, invalidQuery("cursor is not valid for this query; start from the first page")
	}
	return &usecase.AdminCursor{CreatedAt: c.T, ID: c.ID}, nil
}

// parseTimeRange membaca from/to RFC 3339 (from inklusif, to eksklusif), sama dengan export.
func parseTimeRange(c *gin.Context) (from, to *time.Time, err error) {
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &from}, {"to", &to}} {
		if v := c.Query(p.name); v != "" {
			t, perr := time.Parse(time.RFC3339, v)
			if perr != nil {
				return nil, nil, invalidQuery("%s must be an RFC 3339 timestamp", p.name)
			}
			*p.dst = &t
		}
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, nil, invalidQuery("from must be before to")
	}
	return from, to, nil
}

func positiveInt(c *gin.Context, name string) (*int, error) {
	v := c.Query(name)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return nil, invalidQuery("%s must be a positive integer", name)
	}
	return &n, nil
}

func parseAdminListQuery(c *gin.Context) (usecase.AdminListQuery, error) {
	var q usecase.AdminListQuery
	var err error
	if q.From, q.To, err = parseTimeRange(c); err != nil {
		return q, err
	}
	if q.LabelID, err = positiveInt(c, "label_id"); err != nil {
		return q, err
	}
	if v := c.Query("user_id"); v != "" {
		id, perr := uuid.Parse(v)
		if perr != nil {
			return q, invalidQuery("user_id must be a UUID")
		}
		q.UserID = &id
	}
	if v := c.Query("q"); v != "" {
		if !utf8.ValidString(v) || utf8.RuneCountInString(v) > 100 {
			return q, invalidQuery("q must be at most 100 characters")
		}
		q.NameQ = v
		if p, ok := domain.PhonePrefix(v); ok {
			q.PhoneQ = p
		}
	}
	if v := c.Query("unlabeled"); v != "" {
		b, perr := strconv.ParseBool(v)
		if perr != nil {
			return q, invalidQuery("unlabeled must be true or false")
		}
		q.Unlabeled = b
	}
	switch c.Query("sort") {
	case "", "newest":
	case "oldest":
		q.Oldest = true
	default:
		return q, invalidQuery("sort must be newest or oldest")
	}
	if v := c.Query("limit"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 || n > usecase.AdminListMaxLimit {
			return q, invalidQuery("limit must be between 1 and %d", usecase.AdminListMaxLimit)
		}
		q.Limit = n
	}
	if v := c.Query("cursor"); v != "" {
		if q.After, err = decodeCursor(v, q.Oldest); err != nil {
			return q, err
		}
	}
	return q, nil
}

type reviewDTO struct {
	Status     string     `json:"status"`
	Reason     *string    `json:"reason"`
	ReviewedAt *time.Time `json:"reviewed_at"`
}

// reviewOf: status review submission (spec-labels-admin §5.7).
func reviewOf(domain.Submission) reviewDTO {
	return reviewDTO{Status: "pending"}
}

type annotatorDTO struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	Phone   string    `json:"phone"` // utuh: admin perlu bisa menghubungi annotator (§8)
	Blocked bool      `json:"blocked"`
}

func toAnnotatorDTO(a *usecase.Annotator) *annotatorDTO {
	if a == nil {
		return nil
	}
	return &annotatorDTO{ID: a.ID, Name: a.Name, Email: a.Email, Phone: a.Phone, Blocked: a.Blocked}
}

type labelCountDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count"`
}

type coverBoxDTO struct {
	LabelID *int    `json:"label_id"` // null untuk kotak teks lama
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
}

type coverDTO struct {
	PhotoID     uuid.UUID     `json:"photo_id"`
	URL         string        `json:"url"`
	Width       int           `json:"width"`
	Height      int           `json:"height"`
	Annotations []coverBoxDTO `json:"annotations"`
}

type adminSubmissionItemDTO struct {
	ID                 uuid.UUID       `json:"id"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	Version            int             `json:"version"`
	Review             reviewDTO       `json:"review"`
	RetentionDeletesAt *time.Time      `json:"retention_deletes_at"`
	Annotator          *annotatorDTO   `json:"annotator"`
	PhotoCount         int             `json:"photo_count"`
	AnnotationCount    int             `json:"annotation_count"`
	Labels             []labelCountDTO `json:"labels"`
	LegacyTextCount    int             `json:"legacy_text_count"`
	Cover              *coverDTO       `json:"cover"`
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func toAdminItem(it usecase.AdminListItem) adminSubmissionItemDTO {
	s := it.Submission
	out := adminSubmissionItemDTO{
		ID: s.ID, CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(), Version: s.Version,
		Review: reviewOf(s), RetentionDeletesAt: utcPtr(it.RetentionDeletesAt), Annotator: toAnnotatorDTO(it.Annotator),
		PhotoCount: it.PhotoCount, AnnotationCount: it.AnnotationCount, LegacyTextCount: it.LegacyTextCount,
		Labels: make([]labelCountDTO, len(it.Labels)),
	}
	for i, l := range it.Labels {
		out.Labels[i] = labelCountDTO{ID: l.ID, Name: l.Name, Color: l.Color, Count: l.Count}
	}
	if it.Cover != nil {
		cv := &coverDTO{PhotoID: it.Cover.Photo.ID, URL: it.CoverURL, Width: it.Cover.Photo.Width, Height: it.Cover.Photo.Height,
			Annotations: make([]coverBoxDTO, len(it.Cover.Annotations))}
		for i, a := range it.Cover.Annotations {
			b := coverBoxDTO{X: a.X, Y: a.Y, W: a.W, H: a.H}
			if a.LabelID != 0 {
				id := a.LabelID
				b.LabelID = &id
			}
			cv.Annotations[i] = b
		}
		out.Cover = cv
	}
	return out
}

// List menangani GET /api/v1/admin/submissions.
func (h *AdminSubmissionHandler) List(c *gin.Context) {
	q, err := parseAdminListQuery(c)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	res, err := h.reader.List(c.Request.Context(), q)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	items := make([]adminSubmissionItemDTO, len(res.Items))
	for i, it := range res.Items {
		items[i] = toAdminItem(it)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(res.Next, q.Oldest)})
}

type adminPhotoDTO struct {
	photoViewDTO
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type adminSubmissionDetailDTO struct {
	ID                 uuid.UUID       `json:"id"`
	Version            int             `json:"version"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	EditableUntil      time.Time       `json:"editable_until"`
	Editable           bool            `json:"editable"`
	Review             reviewDTO       `json:"review"`
	RetentionDeletesAt *time.Time      `json:"retention_deletes_at"`
	Annotator          *annotatorDTO   `json:"annotator"`
	URLExpiresAt       time.Time       `json:"url_expires_at"`
	Photos             []adminPhotoDTO `json:"photos"`
}

// Detail menangani GET /api/v1/admin/submissions/{id}.
func (h *AdminSubmissionHandler) Detail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	d, err := h.reader.Detail(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	s := d.View.Detail.Submission
	out := adminSubmissionDetailDTO{
		ID: s.ID, Version: s.Version, CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(),
		EditableUntil: d.View.EditableUntil.UTC(), Editable: d.View.Editable,
		Review: reviewOf(s), RetentionDeletesAt: utcPtr(d.RetentionDeletesAt), Annotator: toAnnotatorDTO(d.Annotator),
		URLExpiresAt: d.URLExpiresAt.UTC(), Photos: make([]adminPhotoDTO, len(d.View.Detail.Photos)),
	}
	for i, p := range d.View.Detail.Photos {
		out.Photos[i] = adminPhotoDTO{photoViewDTO: toPhotoView(p), URL: d.PhotoURLs[p.Photo.ID], SHA256: hex.EncodeToString(p.Photo.SHA256)}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}
