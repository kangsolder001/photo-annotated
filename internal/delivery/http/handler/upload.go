package handler

import (
	"context"
	"net/http"
	"strconv"
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

// Uploader dipenuhi oleh *usecase.Upload.
type Uploader interface {
	Upload(ctx context.Context, in usecase.UploadInput) error
}

type UploadHandler struct {
	presign        Presigner
	uploader       Uploader
	maxUploadBytes int64
}

func NewUploadHandler(p Presigner, u Uploader, maxUploadBytes int64) *UploadHandler {
	return &UploadHandler{presign: p, uploader: u, maxUploadBytes: maxUploadBytes}
}

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

// Put menangani PUT /api/v1/uploads/{photo_id}?exp=&sig= (spec §5.1b): byte foto mentah,
// diotorisasi oleh tanda tangan URL (tanpa header Authorization).
func (h *UploadHandler) Put(c *gin.Context) {
	id, err := uuid.Parse(c.Param("photo_id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	// Tolak sejak awal body yang jelas melebihi batas, tanpa membaca byte-nya.
	if c.Request.ContentLength > h.maxUploadBytes {
		httpx.AbortError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "file is larger than the allowed size")
		return
	}
	// exp tak valid (hilang, bukan angka, atau di luar rentang int64) dijadikan 0: tanda tangan
	// tidak akan cocok, sehingga ditolak 403. ParseInt mengembalikan MaxInt64 bersama error saat
	// overflow, jadi errornya harus diperiksa, bukan dibuang.
	exp, err := strconv.ParseInt(c.Query("exp"), 10, 64)
	if err != nil {
		exp = 0
	}
	err = h.uploader.Upload(c.Request.Context(), usecase.UploadInput{
		PhotoID:       id,
		Exp:           exp,
		Sig:           c.Query("sig"),
		ContentType:   c.GetHeader("Content-Type"),
		ContentLength: c.Request.ContentLength,
		Body:          c.Request.Body,
	})
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

// Presign menangani POST /api/v1/uploads/presign (spec §5.1).
func (h *UploadHandler) Presign(c *gin.Context) {
	var req presignRequest
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	out, err := h.presign.Presign(c.Request.Context(), usecase.PresignInput{
		ContentType: req.ContentType, SizeBytes: req.SizeBytes, Width: req.Width, Height: req.Height,
		OwnerID: sessionUser(c),
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
