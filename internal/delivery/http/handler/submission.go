package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// Submitter dipenuhi oleh *usecase.Submit.
type Submitter interface {
	Submit(ctx context.Context, in usecase.SubmitInput) (*usecase.SubmitOutput, error)
}

type SubmissionHandler struct{ submitter Submitter }

func NewSubmissionHandler(s Submitter) *SubmissionHandler { return &SubmissionHandler{submitter: s} }

// annotationInDTO adalah kotak yang dikirim klien (spec-labels-admin §5.1). Field "text" tidak lagi
// dikenal sehingga ditolak 400 oleh DecodeJSON.
type annotationInDTO struct {
	LabelID int     `json:"label_id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
}

type submitPhotoDTO struct {
	PhotoID     uuid.UUID         `json:"photo_id"`
	Annotations []annotationInDTO `json:"annotations"`
}

// toSubmitPhotos memetakan foto dari request ke input usecase (dipakai POST dan PUT).
func toSubmitPhotos(in []submitPhotoDTO) []usecase.SubmitPhoto {
	out := make([]usecase.SubmitPhoto, len(in))
	for i, p := range in {
		sp := usecase.SubmitPhoto{PhotoID: p.PhotoID, Annotations: make([]domain.Annotation, len(p.Annotations))}
		for j, a := range p.Annotations {
			sp.Annotations[j] = domain.Annotation{LabelID: a.LabelID, X: a.X, Y: a.Y, W: a.W, H: a.H}
		}
		out[i] = sp
	}
	return out
}

type submitRequest struct {
	ClientRequestID uuid.UUID                  `json:"client_request_id"`
	FormVersion     int                        `json:"form_version"`
	Answers         map[string]json.RawMessage `json:"answers"`
	Photos          []submitPhotoDTO           `json:"photos"`
}

type submitResponse struct {
	SubmissionID uuid.UUID `json:"submission_id"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	EditToken    string    `json:"edit_token,omitempty"`
}

// Create menangani POST /api/v1/submissions (spec §5.2). Respons 201 untuk submission baru;
// 200 bila client_request_id sudah pernah dipakai (permintaan ulang idempoten).
func (h *SubmissionHandler) Create(c *gin.Context) {
	var req submitRequest
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	in := usecase.SubmitInput{ClientRequestID: req.ClientRequestID, OwnerID: sessionUser(c), Photos: toSubmitPhotos(req.Photos),
		FormVersion: req.FormVersion, Answers: req.Answers}
	if ip, err := netip.ParseAddr(httpx.ClientIP(c)); err == nil {
		in.ClientIP = &ip
	}

	out, err := h.submitter.Submit(c.Request.Context(), in)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	status := http.StatusCreated
	if out.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, submitResponse{
		SubmissionID: out.SubmissionID, Version: out.Version, CreatedAt: out.CreatedAt.UTC(), EditToken: out.EditToken,
	})
}
