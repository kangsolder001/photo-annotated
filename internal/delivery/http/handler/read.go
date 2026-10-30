package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// Reader dipenuhi oleh *usecase.Reader.
type Reader interface {
	Submission(ctx context.Context, c usecase.Caller, id uuid.UUID) (*usecase.SubmissionView, error)
	PhotoURL(ctx context.Context, c usecase.Caller, photoID uuid.UUID) (*usecase.PhotoURL, error)
	PhotoContent(ctx context.Context, photoID uuid.UUID, exp int64, sig string) (*usecase.PhotoContentResult, error)
}

type ReadHandler struct{ reader Reader }

func NewReadHandler(r Reader) *ReadHandler { return &ReadHandler{reader: r} }

// callerFrom mengumpulkan kredensial request. Saat ini hanya token edit; identitas user dan
// admin ditambahkan oleh middleware autentikasi (T22–T24).
func callerFrom(c *gin.Context) usecase.Caller {
	return usecase.Caller{EditToken: c.GetHeader("X-Edit-Token"), UserID: sessionUser(c), Admin: httpx.IsAdmin(c)}
}

// sessionUser mengembalikan pengguna sesi (auth.mode = otp), atau nil untuk request anonim.
func sessionUser(c *gin.Context) *uuid.UUID {
	if id, _, ok := httpx.Session(c); ok {
		return &id
	}
	return nil
}

// labelRefDTO adalah label sebuah kotak, termasuk label yang sudah diarsipkan.
type labelRefDTO struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Archived bool   `json:"archived"`
}

// annotationDTO adalah kotak saat dibaca: label lengkap (nama/warna terkini) atau, untuk baris lama
// dari masa teks bebas, label null dan text berisi catatannya (spec-labels-admin §4.3, §5.1).
type annotationDTO struct {
	Label *labelRefDTO `json:"label"`
	Text  *string      `json:"text"`
	X     float64      `json:"x"`
	Y     float64      `json:"y"`
	W     float64      `json:"w"`
	H     float64      `json:"h"`
}

func toAnnotationDTO(a domain.Annotation) annotationDTO {
	out := annotationDTO{X: a.X, Y: a.Y, W: a.W, H: a.H, Text: optString(a.Text)}
	if a.Label != nil {
		out.Label = &labelRefDTO{ID: a.Label.ID, Name: a.Label.Name, Color: a.Label.Color, Archived: a.Label.Archived()}
	}
	return out
}

type photoViewDTO struct {
	PhotoID     uuid.UUID       `json:"photo_id"`
	Position    int             `json:"position"`
	ContentType string          `json:"content_type"`
	SizeBytes   int64           `json:"size_bytes"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	Annotations []annotationDTO `json:"annotations"`
}

// userReviewDTO: status review yang dilihat pemilik (tanpa reviewed_at, spec-labels-admin §5.7).
type userReviewDTO struct {
	Status string  `json:"status"`
	Reason *string `json:"reason"`
}

// submissionViewDTO sengaja tidak memuat field internal (storage_key, sha256, owner_id, client_ip).
type submissionViewDTO struct {
	ID            uuid.UUID      `json:"id"`
	Version       int            `json:"version"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	EditableUntil time.Time      `json:"editable_until"`
	Editable      bool           `json:"editable"`
	Photos        []photoViewDTO `json:"photos"`
	Review        userReviewDTO  `json:"review"`
	// pendataan (spec-pendataan §5.3); null untuk submission tanpa pendataan
	FormVersion *int           `json:"form_version"`
	Answers     map[string]any `json:"answers"`
	Consent     *bool          `json:"consent"`
}

func toPhotoView(p domain.PhotoWithAnnotations) photoViewDTO {
	pd := photoViewDTO{
		PhotoID: p.Photo.ID, Position: p.Photo.Position, ContentType: p.Photo.ContentType,
		SizeBytes: p.Photo.SizeBytes, Width: p.Photo.Width, Height: p.Photo.Height,
		Annotations: make([]annotationDTO, len(p.Annotations)),
	}
	for j, a := range p.Annotations {
		pd.Annotations[j] = toAnnotationDTO(a)
	}
	return pd
}

// GetSubmission menangani GET /api/v1/submissions/{id} (spec §5.3).
func (h *ReadHandler) GetSubmission(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	v, err := h.reader.Submission(c.Request.Context(), callerFrom(c), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	dto := submissionViewDTO{
		ID: v.Detail.Submission.ID, Version: v.Detail.Submission.Version,
		CreatedAt: v.Detail.Submission.CreatedAt.UTC(), UpdatedAt: v.Detail.Submission.UpdatedAt.UTC(),
		EditableUntil: v.EditableUntil.UTC(), Editable: v.Editable,
		Photos:      make([]photoViewDTO, len(v.Detail.Photos)),
		FormVersion: formVersion(v.Detail.Submission), Answers: v.Detail.Submission.Answers, Consent: surveyConsent(v.Detail.Submission),
	}
	rv := toReviewDTO(v.Detail.Submission.Review)
	dto.Review = userReviewDTO{Status: rv.Status, Reason: rv.Reason}
	for i, p := range v.Detail.Photos {
		dto.Photos[i] = toPhotoView(p)
	}
	c.JSON(http.StatusOK, dto)
}

// PhotoURL menangani GET /api/v1/photos/{id}/url (spec §5.5).
func (h *ReadHandler) PhotoURL(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	u, err := h.reader.PhotoURL(c.Request.Context(), callerFrom(c), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": u.URL, "expires_at": u.ExpiresAt.UTC()})
}

// PhotoContent menangani GET /api/v1/photos/{id}/content?exp=&sig= (spec §5.5). Otorisasi hanya
// tanda tangan URL (tanpa header Authorization) agar bisa langsung dipakai di <img src>.
func (h *ReadHandler) PhotoContent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	exp, perr := strconv.ParseInt(c.Query("exp"), 10, 64)
	if perr != nil {
		exp = 0 // ditolak sebagai tanda tangan tidak cocok
	}
	res, err := h.reader.PhotoContent(c.Request.Context(), id, exp, c.Query("sig"))
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	defer res.File.Close()

	hdr := c.Writer.Header()
	hdr.Set("Content-Type", res.ContentType) // diset eksplisit agar ServeContent tidak menebak
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Disposition", "inline")
	hdr.Set("Cache-Control", "private, max-age=300")
	if res.ETag != "" {
		hdr.Set("ETag", res.ETag)
	}
	// ServeContent menangani Range, If-Range, If-None-Match, dan If-Modified-Since.
	http.ServeContent(c.Writer, c.Request, "", res.File.Info().ModTime, res.File)
}
