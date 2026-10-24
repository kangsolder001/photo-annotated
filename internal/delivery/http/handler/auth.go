package handler

import (
	"context"
	"math"
	"net/http"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// OTPRequester dipenuhi oleh *usecase.OTPRequest.
type OTPRequester interface {
	Register(ctx context.Context, in usecase.RegisterInput) (*usecase.OTPTicket, error)
	Request(ctx context.Context, phone string, clientIP *netip.Addr) (*usecase.OTPTicket, error)
	Status(ctx context.Context, id uuid.UUID) (*usecase.OTPStatusView, error)
}

// ChannelStatus dipenuhi oleh *usecase.Channel.
type ChannelStatus interface{ Available() bool }

// OTPVerifier dipenuhi oleh *usecase.OTPVerify.
type OTPVerifier interface {
	Verify(ctx context.Context, in usecase.VerifyInput) (*usecase.LoginResult, error)
}

// SessionService dipenuhi oleh *usecase.Sessions.
type SessionService interface {
	Logout(ctx context.Context, sessionID uuid.UUID) error
	Me(ctx context.Context, userID uuid.UUID) (*domain.User, error)
}

// AuthDeps adalah dependensi AuthHandler.
type AuthDeps struct {
	OTP       OTPRequester
	Verifier  OTPVerifier
	Sessions  SessionService
	Channel   ChannelStatus
	Turnstile usecase.TurnstileVerifier // nil bila turnstile.enabled = false
}

type AuthHandler struct {
	otp       OTPRequester
	verifier  OTPVerifier
	sessions  SessionService
	channel   ChannelStatus
	turnstile usecase.TurnstileVerifier
}

func NewAuthHandler(d AuthDeps) *AuthHandler {
	return &AuthHandler{otp: d.OTP, verifier: d.Verifier, sessions: d.Sessions, channel: d.Channel, turnstile: d.Turnstile}
}

type registerRequest struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
	TurnstileToken string `json:"turnstile_token"`
}

type otpRequestRequest struct {
	Phone          string `json:"phone"`
	TurnstileToken string `json:"turnstile_token"`
}

type otpTicketResponse struct {
	ChallengeID          uuid.UUID `json:"challenge_id"`
	Status               string    `json:"status"`
	ResendAfterSeconds   int       `json:"resend_after_seconds"`
	EstimatedWaitSeconds int       `json:"estimated_wait_seconds"`
}

func seconds(d time.Duration) int { return int(math.Ceil(d.Seconds())) }

// verifyHuman menjalankan Turnstile bila diaktifkan. Bila gagal, respons sudah ditulis.
func (h *AuthHandler) verifyHuman(c *gin.Context, token string) bool {
	if h.turnstile == nil {
		return true
	}
	if err := h.turnstile.Verify(c.Request.Context(), token, httpx.ClientIP(c)); err != nil {
		httpx.WriteError(c, err)
		return false
	}
	return true
}

func clientAddr(c *gin.Context) *netip.Addr {
	if ip, err := netip.ParseAddr(httpx.ClientIP(c)); err == nil {
		return &ip
	}
	return nil
}

func writeTicket(c *gin.Context, t *usecase.OTPTicket, err error) {
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, otpTicketResponse{
		ChallengeID: t.ChallengeID, Status: "queued",
		ResendAfterSeconds: seconds(t.ResendAfter), EstimatedWaitSeconds: seconds(t.EstimatedWait),
	})
}

// Register menangani POST /api/v1/auth/register (spec §5.8).
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if !httpx.DecodeJSON(c, &req) || !h.verifyHuman(c, req.TurnstileToken) {
		return
	}
	t, err := h.otp.Register(c.Request.Context(), usecase.RegisterInput{
		Name: req.Name, Email: req.Email, Phone: req.Phone, ClientIP: clientAddr(c),
	})
	writeTicket(c, t, err)
}

// RequestOTP menangani POST /api/v1/auth/otp/request (spec §5.8): login dan kirim ulang.
func (h *AuthHandler) RequestOTP(c *gin.Context) {
	var req otpRequestRequest
	if !httpx.DecodeJSON(c, &req) || !h.verifyHuman(c, req.TurnstileToken) {
		return
	}
	t, err := h.otp.Request(c.Request.Context(), req.Phone, clientAddr(c))
	writeTicket(c, t, err)
}

type otpStatusResponse struct {
	Status             string     `json:"status"`
	FailureReason      *string    `json:"failure_reason"`
	ResendAfterSeconds int        `json:"resend_after_seconds"`
	ExpiresAt          *time.Time `json:"expires_at"`
}

// OTPStatus menangani GET /api/v1/auth/otp/{challenge_id} (spec §5.8), dipantau FE tiap 2 detik.
// Id yang tidak dikenal (termasuk yang bukan UUID) dijawab queued agar tidak membuka informasi.
func (h *AuthHandler) OTPStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		id = uuid.Nil
	}
	v, err := h.otp.Status(c.Request.Context(), id)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	out := otpStatusResponse{Status: string(v.Status), ResendAfterSeconds: seconds(v.ResendAfter)}
	if v.FailureReason != "" {
		out.FailureReason = &v.FailureReason
	}
	if v.ExpiresAt != nil {
		t := v.ExpiresAt.UTC()
		out.ExpiresAt = &t
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

// ChannelStatus menangani GET /api/v1/auth/channel-status: apakah pengiriman kode WhatsApp sedang
// dapat dipakai (spec §9.5).
func (h *AuthHandler) ChannelStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"otp_available": h.channel.Available()})
}

type verifyRequest struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Code        string    `json:"code"`
}

type userDTO struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Phone string    `json:"phone"` // disamarkan
	Role  string    `json:"role"`
}

func toUserDTO(u *domain.User) userDTO {
	return userDTO{ID: u.ID, Name: u.Name, Email: u.Email, Phone: domain.MaskPhone(u.Phone), Role: "user"}
}

type loginResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	User        userDTO   `json:"user"`
}

// Verify menangani POST /api/v1/auth/otp/verify (spec §5.8).
func (h *AuthHandler) Verify(c *gin.Context) {
	var req verifyRequest
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	res, err := h.verifier.Verify(c.Request.Context(), usecase.VerifyInput{ChallengeID: req.ChallengeID, Code: req.Code, ClientIP: clientAddr(c)})
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, loginResponse{AccessToken: res.Token, ExpiresAt: res.ExpiresAt.UTC(), User: toUserDTO(res.User)})
}

// Me menangani GET /api/v1/auth/me (butuh sesi).
func (h *AuthHandler) Me(c *gin.Context) {
	userID, _, ok := httpx.Session(c)
	if !ok {
		httpx.WriteError(c, domain.Unauthorized("UNAUTHORIZED", "missing, invalid, or expired session"))
		return
	}
	u, err := h.sessions.Me(c.Request.Context(), userID)
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, toUserDTO(u))
}

// Logout menangani POST /api/v1/auth/logout: mencabut sesi saat ini (butuh sesi).
func (h *AuthHandler) Logout(c *gin.Context) {
	_, sessionID, ok := httpx.Session(c)
	if !ok {
		httpx.WriteError(c, domain.Unauthorized("UNAUTHORIZED", "missing, invalid, or expired session"))
		return
	}
	if err := h.sessions.Logout(c.Request.Context(), sessionID); err != nil {
		httpx.WriteError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
