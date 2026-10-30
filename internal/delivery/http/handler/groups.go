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

// GroupService dipenuhi oleh *usecase.Groups.
type GroupService interface {
	Active(ctx context.Context) ([]domain.Group, error)
	All(ctx context.Context) ([]usecase.GroupUsage, error)
	Create(ctx context.Context, name string) (*usecase.GroupUsage, error)
	Update(ctx context.Context, id int, name string, archived bool) (*usecase.GroupUsage, error)
	Delete(ctx context.Context, id int) (*domain.Group, error)
}

type GroupHandler struct {
	groups GroupService
	log    *slog.Logger
}

func NewGroupHandler(g GroupService, log *slog.Logger) *GroupHandler {
	return &GroupHandler{groups: g, log: log}
}

type groupDTO struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Active menangani GET /api/v1/groups (publik, dipakai form registrasi): kelompok aktif urut nama.
func (h *GroupHandler) Active(c *gin.Context) {
	gs, err := h.groups.Active(c.Request.Context())
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	out := make([]groupDTO, len(gs))
	for i, g := range gs {
		out[i] = groupDTO{ID: g.ID, Name: g.Name}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"groups": out})
}

type adminGroupDTO struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Archived     bool      `json:"archived"`
	StudentCount int       `json:"student_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toAdminGroup(g usecase.GroupUsage) adminGroupDTO {
	return adminGroupDTO{ID: g.ID, Name: g.Name, Archived: g.Archived(), StudentCount: g.StudentCount,
		CreatedAt: g.CreatedAt.UTC(), UpdatedAt: g.UpdatedAt.UTC()}
}

// AdminList menangani GET /api/v1/admin/groups: semua kelompok termasuk arsip, dengan student_count.
func (h *GroupHandler) AdminList(c *gin.Context) {
	gs, err := h.groups.All(c.Request.Context())
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	out := make([]adminGroupDTO, len(gs))
	for i, g := range gs {
		out[i] = toAdminGroup(g)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"groups": out})
}

// Create menangani POST /api/v1/admin/groups.
func (h *GroupHandler) Create(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	g, err := h.groups.Create(c.Request.Context(), req.Name)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "group.create", "group_id", g.ID)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, toAdminGroup(*g))
}

// groupID membaca {id}; bukan bilangan bulat positif → 404 (sama dengan id yang tidak ada).
func groupID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		httpx.WriteError(c, domain.NotFound("NOT_FOUND", "group not found"))
		return 0, false
	}
	return id, true
}

// Update menangani PUT /api/v1/admin/groups/{id} (ganti nama, arsip / buka arsip).
func (h *GroupHandler) Update(c *gin.Context) {
	id, ok := groupID(c)
	if !ok {
		return
	}
	var req struct {
		Name     string `json:"name"`
		Archived bool   `json:"archived"`
	}
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	g, err := h.groups.Update(c.Request.Context(), id, req.Name, req.Archived)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "group.update", "group_id", id, "archived", g.Archived())
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, toAdminGroup(*g))
}

// Delete menangani DELETE /api/v1/admin/groups/{id}: hanya kelompok yang belum dipakai akun.
func (h *GroupHandler) Delete(c *gin.Context) {
	id, ok := groupID(c)
	if !ok {
		return
	}
	if _, err := h.groups.Delete(c.Request.Context(), id); err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "group.delete", "group_id", id)
	c.Status(http.StatusNoContent)
}
