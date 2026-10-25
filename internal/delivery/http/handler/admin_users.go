package handler

import (
	"context"
	"log/slog"
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

// AdminUserService dipenuhi oleh *usecase.AdminUsers.
type AdminUserService interface {
	List(ctx context.Context, q usecase.AdminUserQuery) (*usecase.AdminUserList, error)
	Get(ctx context.Context, id uuid.UUID) (*usecase.AdminUserRow, error)
	Block(ctx context.Context, id uuid.UUID, reason string) (*usecase.BlockResult, error)
	Unblock(ctx context.Context, id uuid.UUID) (*usecase.BlockResult, error)
	DeleteData(ctx context.Context, id uuid.UUID, includeAccepted bool) (*usecase.UserDataMark, error)
	DeleteAccount(ctx context.Context, id uuid.UUID) (*usecase.UserDataMark, error)
}

// AdminUserHandler menangani kelola annotator (spec-labels-admin §5.8). Respons memuat data pribadi:
// Cache-Control no-store. Alasan blokir tidak di-log.
type AdminUserHandler struct {
	users AdminUserService
	log   *slog.Logger
}

func NewAdminUserHandler(u AdminUserService, log *slog.Logger) *AdminUserHandler {
	return &AdminUserHandler{users: u, log: log}
}

type userStatsDTO struct {
	Submissions      int        `json:"submissions"`
	Annotations      int        `json:"annotations"`
	Accepted         int        `json:"accepted"`
	Rejected         int        `json:"rejected"`
	Pending          int        `json:"pending"`
	LastSubmissionAt *time.Time `json:"last_submission_at"`
}

type adminUserDTO struct {
	ID            uuid.UUID    `json:"id"`
	Name          string       `json:"name"`
	Email         string       `json:"email"`
	Phone         string       `json:"phone"` // utuh (dashboard admin)
	CreatedAt     time.Time    `json:"created_at"`
	LastLoginAt   *time.Time   `json:"last_login_at"`
	Blocked       bool         `json:"blocked"`
	BlockedAt     *time.Time   `json:"blocked_at"`
	BlockedReason *string      `json:"blocked_reason"`
	Stats         userStatsDTO `json:"stats"`
}

func toAdminUser(r *usecase.AdminUserRow) adminUserDTO {
	u := r.User
	return adminUserDTO{
		ID: u.ID, Name: u.Name, Email: u.Email, Phone: u.Phone, CreatedAt: u.CreatedAt.UTC(), LastLoginAt: utcPtr(u.LastLoginAt),
		Blocked: u.Blocked(), BlockedAt: utcPtr(u.BlockedAt), BlockedReason: optString(u.BlockedReason),
		Stats: userStatsDTO{Submissions: r.Stats.Submissions, Annotations: r.Stats.Annotations, Accepted: r.Stats.Accepted,
			Rejected: r.Stats.Rejected, Pending: r.Stats.Pending, LastSubmissionAt: utcPtr(r.Stats.LastSubmissionAt)},
	}
}

func parseUserQuery(c *gin.Context) (usecase.AdminUserQuery, error) {
	var q usecase.AdminUserQuery
	if v := c.Query("q"); v != "" {
		if !utf8.ValidString(v) || utf8.RuneCountInString(v) > 100 {
			return q, invalidQuery("q must be at most 100 characters")
		}
		q.NameQ = v
		if p, ok := domain.PhonePrefix(v); ok {
			q.PhoneQ = p
		}
	}
	switch c.Query("status") {
	case "":
	case "active":
		f := false
		q.Blocked = &f
	case "blocked":
		t := true
		q.Blocked = &t
	default:
		return q, invalidQuery("status must be active or blocked")
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > usecase.AdminListMaxLimit {
			return q, invalidQuery("limit must be between 1 and %d", usecase.AdminListMaxLimit)
		}
		q.Limit = n
	}
	if v := c.Query("cursor"); v != "" {
		after, err := decodeCursor(v, false)
		if err != nil {
			return q, err
		}
		q.After = after
	}
	return q, nil
}

// List menangani GET /api/v1/admin/users.
func (h *AdminUserHandler) List(c *gin.Context) {
	q, err := parseUserQuery(c)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	res, err := h.users.List(c.Request.Context(), q)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	items := make([]adminUserDTO, len(res.Items))
	for i := range res.Items {
		items[i] = toAdminUser(&res.Items[i])
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(res.Next, false)})
}

func userID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.NotFound(c)
		return uuid.Nil, false
	}
	return id, true
}

func writeUser(c *gin.Context, r *usecase.AdminUserRow) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, toAdminUser(r))
}

// Get menangani GET /api/v1/admin/users/{id}.
func (h *AdminUserHandler) Get(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	r, err := h.users.Get(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	writeUser(c, r)
}

// Block menangani POST /api/v1/admin/users/{id}/block.
func (h *AdminUserHandler) Block(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	r, err := h.users.Block(c.Request.Context(), id, req.Reason)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "user.block", "user_id", id, "changed", r.Changed)
	writeUser(c, r.User)
}

// Unblock menangani POST /api/v1/admin/users/{id}/unblock.
func (h *AdminUserHandler) Unblock(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	r, err := h.users.Unblock(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "user.unblock", "user_id", id, "changed", r.Changed)
	writeUser(c, r.User)
}

// DeleteData menangani POST /api/v1/admin/users/{id}/delete-data: 202, file dibersihkan cleanup.
func (h *AdminUserHandler) DeleteData(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	var req struct {
		IncludeAccepted bool `json:"include_accepted"`
	}
	if c.Request.ContentLength != 0 && !httpx.DecodeJSON(c, &req) {
		return
	}
	m, err := h.users.DeleteData(c.Request.Context(), id, req.IncludeAccepted)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "user.delete_data", "user_id", id, "include_accepted", req.IncludeAccepted,
		"submissions", m.Submissions, "photos", m.Photos, "annotations", m.Annotations)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, gin.H{"submissions": m.Submissions, "photos": m.Photos, "annotations": m.Annotations, "status": usecase.DeleteStatusCleanupPending})
}

// DeleteAccount menangani DELETE /api/v1/admin/users/{id}: akun + semua data. 202 bila ada file yang
// menunggu cleanup, 200 bila tidak ada foto sama sekali.
func (h *AdminUserHandler) DeleteAccount(c *gin.Context) {
	id, ok := userID(c)
	if !ok {
		return
	}
	m, err := h.users.DeleteAccount(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	logAdminAction(h.log, c, "user.delete_account", "user_id", id,
		"submissions", m.Submissions, "photos", m.Photos, "annotations", m.Annotations)
	code, status := http.StatusAccepted, usecase.DeleteStatusCleanupPending
	if m.Photos == 0 {
		code, status = http.StatusOK, usecase.DeleteStatusDeleted
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(code, gin.H{"account_deleted": true, "submissions": m.Submissions, "photos": m.Photos, "annotations": m.Annotations, "status": status})
}
