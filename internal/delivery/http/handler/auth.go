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

type AuthHandler struct {
	otp       OTPRequester
	channel   ChannelStatus
	turnstile usecase.TurnstileVerifier // nil bila turnstile.enabled = false
}

func NewAuthHandler(otp OTPRequester, channel ChannelStatus, turnstile usecase.TurnstileVerifier) *AuthHandler {
	return &AuthHandler{otp: otp, channel: channel, turnstile: turnstile}
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
