package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/usecase"
)

// Presigner dipenuhi oleh *usecase.Presign.
type Presigner interface {
	Presign(ctx context.Context, in usecase.PresignInput) (*usecase.PresignOutput, error)
}

type UploadHandler struct{ presign Presigner }

func NewUploadHandler(p Presigner) *UploadHandler { return &UploadHandler{presign: p} }

type presignRequest struct {
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type presignResponse struct {
	PhotoID uuid.UUID     `json:"photo_id"`
	Upload  uploadDetails `json:"upload"`
}

type uploadDetails struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// Presign menangani POST /api/v1/uploads/presign (spec §5.1).
func (h *UploadHandler) Presign(c *gin.Context) {
	var req presignRequest
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	out, err := h.presign.Presign(c.Request.Context(), usecase.PresignInput{
		ContentType: req.ContentType, SizeBytes: req.SizeBytes, Width: req.Width, Height: req.Height,
	})
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.JSON(http.StatusCreated, presignResponse{
		PhotoID: out.PhotoID,
		Upload: uploadDetails{
			Method: out.Method, URL: out.URL, Headers: out.Headers, ExpiresAt: out.ExpiresAt.UTC(),
		},
	})
}
