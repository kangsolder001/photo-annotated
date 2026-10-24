package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/usecase"
)

// AdminDeleter dipenuhi oleh *usecase.AdminDelete.
type AdminDeleter interface {
	DeleteSubmission(ctx context.Context, id uuid.UUID) (*usecase.AdminDeleteResult, error)
	DeletePhoto(ctx context.Context, photoID uuid.UUID) (*usecase.AdminDeleteResult, error)
	DeleteMany(ctx context.Context, ids []uuid.UUID) ([]usecase.AdminDeleteResult, error)
}

// AdminDeleteHandler menangani hapus permanen (spec-labels-admin §5.5).
type AdminDeleteHandler struct {
	del AdminDeleter
	log *slog.Logger
}

func NewAdminDeleteHandler(d AdminDeleter, log *slog.Logger) *AdminDeleteHandler {
	return &AdminDeleteHandler{del: d, log: log}
}

type deleteResultDTO struct {
	Status             string `json:"status"`
	PhotosDeleted      int    `json:"photos_deleted"`
	AnnotationsDeleted int    `json:"annotations_deleted"`
	SubmissionDeleted  bool   `json:"submission_deleted"`
}

func (h *AdminDeleteHandler) logResult(c *gin.Context, action string, r *usecase.AdminDeleteResult, extra ...any) {
	logAdminAction(h.log, c, action, append([]any{"submission_id", r.SubmissionID, "status", r.Status,
		"photos_deleted", r.PhotosDeleted, "annotations_deleted", r.AnnotationsDeleted, "submission_deleted", r.SubmissionDeleted,
		"files_deleted", r.FilesDeleted, "file_failures", r.FileFailures}, extra...)...)
}

// writeDeleteResult: 200 bila semua file dan baris sudah terhapus, 202 bila sebagian menunggu cleanup.
func writeDeleteResult(c *gin.Context, r *usecase.AdminDeleteResult) {
	status := http.StatusOK
	if r.Status == usecase.DeleteStatusCleanupPending {
		status = http.StatusAccepted
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, deleteResultDTO{Status: r.Status, PhotosDeleted: r.PhotosDeleted, AnnotationsDeleted: r.AnnotationsDeleted,
		SubmissionDeleted: r.SubmissionDeleted})
}

// DeleteSubmission menangani DELETE /api/v1/admin/submissions/{id}.
func (h *AdminDeleteHandler) DeleteSubmission(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	r, err := h.del.DeleteSubmission(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	h.logResult(c, "submission.delete", r)
	writeDeleteResult(c, r)
}

// DeletePhoto menangani DELETE /api/v1/admin/photos/{id}.
func (h *AdminDeleteHandler) DeletePhoto(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return
	}
	r, err := h.del.DeletePhoto(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	h.logResult(c, "photo.delete", r, "photo_id", id)
	writeDeleteResult(c, r)
}

type bulkDeleteItemDTO struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
}

// DeleteMany menangani POST /api/v1/admin/submissions/delete: 1–50 id, diproses per submission.
func (h *AdminDeleteHandler) DeleteMany(c *gin.Context) {
	var req struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	res, err := h.del.DeleteMany(c.Request.Context(), req.IDs)
	for i := range res {
		if res[i].Status != usecase.DeleteStatusNotFound {
			h.logResult(c, "submission.delete", &res[i], "bulk", true)
		}
	}
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	out := make([]bulkDeleteItemDTO, len(res))
	for i, r := range res {
		out[i] = bulkDeleteItemDTO{ID: r.SubmissionID, Status: r.Status}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"results": out})
}
