package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// LabelService dipenuhi oleh *usecase.Labels.
type LabelService interface {
	Active(ctx context.Context) ([]domain.Label, error)
	All(ctx context.Context) ([]usecase.LabelUsage, error)
	Create(ctx context.Context, in usecase.LabelInput) (*usecase.LabelUsage, error)
	Update(ctx context.Context, id int, in usecase.LabelInput) (*usecase.LabelChange, error)
	Reorder(ctx context.Context, ids []int) ([]usecase.LabelUsage, error)
	Delete(ctx context.Context, id int) (*domain.Label, error)
}

type LabelHandler struct {
	labels LabelService
	log    *slog.Logger
}

func NewLabelHandler(l LabelService, log *slog.Logger) *LabelHandler {
	return &LabelHandler{labels: l, log: log}
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type labelDTO struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       string  `json:"color"`
}

// Active menangani GET /api/v1/labels: label aktif untuk pemilih label pengguna.
func (h *LabelHandler) Active(c *gin.Context) {
	ls, err := h.labels.Active(c.Request.Context())
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	out := make([]labelDTO, len(ls))
	for i, l := range ls {
		out[i] = labelDTO{ID: l.ID, Name: l.Name, Description: optString(l.Description), Color: l.Color}
	}
	c.Header("Cache-Control", "private, max-age=60")
	c.JSON(http.StatusOK, gin.H{"labels": out})
}

type adminLabelDTO struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	Color       string    `json:"color"`
	SortOrder   int       `json:"sort_order"`
	Archived    bool      `json:"archived"`
	UsageCount  int       `json:"usage_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toAdminLabel(l usecase.LabelUsage) adminLabelDTO {
	return adminLabelDTO{
		ID: l.ID, Name: l.Name, Description: optString(l.Description), Color: l.Color, SortOrder: l.SortOrder,
		Archived: l.Archived(), UsageCount: l.UsageCount, CreatedAt: l.CreatedAt.UTC(), UpdatedAt: l.UpdatedAt.UTC(),
	}
}

func writeAdminLabels(c *gin.Context, ls []usecase.LabelUsage) {
	out := make([]adminLabelDTO, len(ls))
	for i, l := range ls {
		out[i] = toAdminLabel(l)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"labels": out})
}

// AdminList menangani GET /api/v1/admin/labels: semua label termasuk arsip, dengan usage_count.
func (h *LabelHandler) AdminList(c *gin.Context) {
	ls, err := h.labels.All(c.Request.Context())
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	writeAdminLabels(c, ls)
}

type labelBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Archived    bool    `json:"archived"` // hanya PUT
}

func (b labelBody) input() usecase.LabelInput {
	in := usecase.LabelInput{Name: b.Name, Archived: b.Archived}
	if b.Description != nil {
		in.Description = *b.Description
	}
	if b.Color != nil {
		in.Color = *b.Color
	}
	return in
}

// Create menangani POST /api/v1/admin/labels.
func (h *LabelHandler) Create(c *gin.Context) {
	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Color       *string `json:"color"`
	}
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	l, err := h.labels.Create(c.Request.Context(), labelBody{Name: req.Name, Description: req.Description, Color: req.Color}.input())
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "label.create", "label_id", l.ID, "label", l.Name)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, toAdminLabel(*l))
}

// labelID membaca {id}; bukan bilangan bulat positif → 404 (sama dengan id yang tidak ada).
func labelID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		httpx.WriteError(c, domain.NotFound("NOT_FOUND", "label not found"))
		return 0, false
	}
	return id, true
}

// Update menangani PUT /api/v1/admin/labels/{id} (replace; juga arsip / buka arsip).
func (h *LabelHandler) Update(c *gin.Context) {
	id, ok := labelID(c)
	if !ok {
		return
	}
	var req labelBody
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	ch, err := h.labels.Update(c.Request.Context(), id, req.input())
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	action := "label.update"
	switch {
	case ch.Archived:
		action = "label.archive"
	case ch.Unarchived:
		action = "label.unarchive"
	}
	logAdminAction(h.log, c, action, "label_id", id, "label", ch.Label.Name, "usage_count", ch.Label.UsageCount)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, toAdminLabel(ch.Label))
}

// Reorder menangani PUT /api/v1/admin/labels/order: urutan baru semua label aktif.
func (h *LabelHandler) Reorder(c *gin.Context) {
	var req struct {
		IDs []int `json:"ids"`
	}
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	ls, err := h.labels.Reorder(c.Request.Context(), req.IDs)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "label.reorder", "count", len(req.IDs))
	writeAdminLabels(c, ls)
}

// Delete menangani DELETE /api/v1/admin/labels/{id}: hanya label yang belum pernah dipakai.
func (h *LabelHandler) Delete(c *gin.Context) {
	id, ok := labelID(c)
	if !ok {
		return
	}
	l, err := h.labels.Delete(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "label.delete", "label_id", id, "label", l.Name)
	c.Status(http.StatusNoContent)
}
