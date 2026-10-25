package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// AdminLoginService dipenuhi oleh *usecase.AdminLogin.
type AdminLoginService interface {
	Request(ctx context.Context, phone string, clientIP *netip.Addr) (*usecase.AdminRequestResult, error)
	Resend(ctx context.Context, challengeID uuid.UUID, clientIP *netip.Addr) (*usecase.OTPTicket, error)
	Status(ctx context.Context, id uuid.UUID) (*usecase.OTPStatusView, error)
	Verify(ctx context.Context, challengeID uuid.UUID, code string) (*usecase.AdminSession, error)
	Profile() usecase.AdminProfile
}

// AdminAuthDeps adalah dependensi AdminAuthHandler.
type AdminAuthDeps struct {
	Login     AdminLoginService
	Channel   ChannelStatus
	Turnstile usecase.TurnstileVerifier // nil bila turnstile.enabled = false
	AuthMode  string                    // auth.mode, dikirim di /admin/me agar FE tahu menu mana yang ada
	Log       *slog.Logger
}

// AdminAuthHandler menangani login superadmin lewat OTP (spec-labels-admin §5.2).
type AdminAuthHandler struct{ d AdminAuthDeps }

func NewAdminAuthHandler(d AdminAuthDeps) *AdminAuthHandler { return &AdminAuthHandler{d: d} }

type adminRequestBody struct {
	Phone          string `json:"phone"`
	TurnstileToken string `json:"turnstile_token"`
}

// Request menangani POST /api/v1/admin/otp/request. Respons sama untuk nomor superadmin maupun bukan.
func (h *AdminAuthHandler) Request(c *gin.Context) {
	var req adminRequestBody
	if !httpx.DecodeJSON(c, &req) || !verifyHuman(c, h.d.Turnstile, req.TurnstileToken) {
		return
	}
	res, err := h.d.Login.Request(c.Request.Context(), req.Phone, clientAddr(c))
	if res != nil {
		attrs := []any{"request_id", httpx.RequestID(c), "stage", "request", "phone", res.MaskedPhone, "superadmin", res.Superadmin}
		if de, ok := domain.AsError(err); ok {
			attrs = append(attrs, "code", de.Code)
		}
		h.d.Log.Info("admin login", attrs...)
	}
	var t *usecase.OTPTicket
	if res != nil {
		t = res.Ticket
	}
	writeTicket(c, t, err)
}

// Resend menangani POST /api/v1/admin/otp/resend.
func (h *AdminAuthHandler) Resend(c *gin.Context) {
	var req resendRequest
	if !httpx.DecodeJSON(c, &req) || !verifyHuman(c, h.d.Turnstile, req.TurnstileToken) {
		return
	}
	t, err := h.d.Login.Resend(c.Request.Context(), req.ChallengeID, clientAddr(c))
	writeTicket(c, t, err)
}

// Status menangani GET /api/v1/admin/otp/{challenge_id} (perilaku sama dengan /auth/otp/{id}).
func (h *AdminAuthHandler) Status(c *gin.Context) {
	writeOTPStatus(c, h.d.Login.Status)
}

type adminVerifyBody struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Code        string    `json:"code"`
}

type adminDTO struct {
	Name  string `json:"name"`
	Phone string `json:"phone"` // disamarkan
}

type adminVerifyResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	Admin       adminDTO  `json:"admin"`
}

// Verify menangani POST /api/v1/admin/otp/verify.
func (h *AdminAuthHandler) Verify(c *gin.Context) {
	var req adminVerifyBody
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	s, err := h.d.Login.Verify(c.Request.Context(), req.ChallengeID, req.Code)
	if err != nil {
		attrs := []any{"request_id", httpx.RequestID(c), "stage", "verify", "ok", false}
		if de, ok := domain.AsError(err); ok {
			attrs = append(attrs, "code", de.Code)
		}
		h.d.Log.Warn("admin login", attrs...)
		httpx.WriteError(c, err)
		return
	}
	h.d.Log.Info("admin login", "request_id", httpx.RequestID(c), "stage", "verify", "ok", true, "phone", s.Admin.MaskedPhone)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, adminVerifyResponse{
		AccessToken: s.Token, ExpiresAt: s.ExpiresAt.UTC(),
		Admin: adminDTO{Name: s.Admin.Name, Phone: s.Admin.MaskedPhone},
	})
}

type adminMeResponse struct {
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	ExpiresAt time.Time `json:"expires_at"`
	AuthMode  string    `json:"auth_mode"`
}

// Me menangani GET /api/v1/admin/me (butuh token admin).
func (h *AdminAuthHandler) Me(c *gin.Context) {
	p := h.d.Login.Profile()
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, adminMeResponse{Name: p.Name, Phone: p.MaskedPhone, ExpiresAt: httpx.AdminExpiry(c).UTC(), AuthMode: h.d.AuthMode})
}

// ChannelStatus menangani GET /api/v1/admin/channel-status: selalu tersedia bila admin aktif, juga
// pada auth.mode = none (di mana /auth/channel-status tidak ada).
func (h *AdminAuthHandler) ChannelStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"otp_available": h.d.Channel.Available()})
}

// logAdminAction mencatat satu baris "admin action" (spec-labels-admin §8). Karena tidak ada audit log
// di database, baris ini satu-satunya jejak siapa mengubah/menghapus apa. Jangan masukkan data pribadi
// atau alasan teks bebas.
func logAdminAction(log *slog.Logger, c *gin.Context, action string, attrs ...any) {
	log.Info("admin action", append([]any{"action", action, "request_id", httpx.RequestID(c)}, attrs...)...)
}
