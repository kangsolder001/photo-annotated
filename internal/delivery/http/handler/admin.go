package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

// AdminLoginer dipenuhi oleh *usecase.AdminAuth.
type AdminLoginer interface {
	Login(ctx context.Context, username, password string) (token string, expiresAt time.Time, err error)
}

type AdminHandler struct{ login AdminLoginer }

func NewAdminHandler(l AdminLoginer) *AdminHandler { return &AdminHandler{login: l} }

type adminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminLoginResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Login menangani POST /api/v1/admin/login (spec §5.9).
func (h *AdminHandler) Login(c *gin.Context) {
	var req adminLoginRequest
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	token, exp, err := h.login.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, adminLoginResponse{AccessToken: token, ExpiresAt: exp.UTC()})
}
