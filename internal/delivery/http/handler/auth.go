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
	Forgot(ctx context.Context, phone string, clientIP *netip.Addr) (*usecase.OTPTicket, error)
	Resend(ctx context.Context, challengeID uuid.UUID, clientIP *netip.Addr) (*usecase.OTPTicket, error)
	Status(ctx context.Context, id uuid.UUID) (*usecase.OTPStatusView, error)
}

// PasswordLoginer dipenuhi oleh *usecase.PasswordLogin.
type PasswordLoginer interface {
	Login(ctx context.Context, phone, password string, clientIP *netip.Addr) (*usecase.LoginResult, error)
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
	Login     PasswordLoginer
	Sessions  SessionService
	Channel   ChannelStatus
	Turnstile usecase.TurnstileVerifier // nil bila turnstile.enabled = false
}

type AuthHandler struct {
	otp       OTPRequester
	verifier  OTPVerifier
	login     PasswordLoginer
	sessions  SessionService
	channel   ChannelStatus
	turnstile usecase.TurnstileVerifier
}

func NewAuthHandler(d AuthDeps) *AuthHandler {
	return &AuthHandler{otp: d.OTP, verifier: d.Verifier, login: d.Login, sessions: d.Sessions, channel: d.Channel, turnstile: d.Turnstile}
}

type registerRequest struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
	Password       string `json:"password"`
	NIM            string `json:"nim"`
	GroupID        int    `json:"group_id"`
	TurnstileToken string `json:"turnstile_token"`
}

type forgotRequest struct {
	Phone          string `json:"phone"`
	TurnstileToken string `json:"turnstile_token"`
}

type resendRequest struct {
	ChallengeID    uuid.UUID `json:"challenge_id"`
	TurnstileToken string    `json:"turnstile_token"`
}

type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

type otpTicketResponse struct {
	ChallengeID          uuid.UUID `json:"challenge_id"`
	Status               string    `json:"status"`
	ResendAfterSeconds   int       `json:"resend_after_seconds"`
	EstimatedWaitSeconds int       `json:"estimated_wait_seconds"`
}

func seconds(d time.Duration) int { return int(math.Ceil(d.Seconds())) }

// verifyHuman menjalankan Turnstile bila diaktifkan (ts tidak nil). Bila gagal, respons sudah ditulis.
func verifyHuman(c *gin.Context, ts usecase.TurnstileVerifier, token string) bool {
	if ts == nil {
		return true
	}
	if err := ts.Verify(c.Request.Context(), token, httpx.ClientIP(c)); err != nil {
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
	if !httpx.DecodeJSON(c, &req) || !verifyHuman(c, h.turnstile, req.TurnstileToken) {
		return
	}
	t, err := h.otp.Register(c.Request.Context(), usecase.RegisterInput{
		Name: req.Name, Email: req.Email, Phone: req.Phone, Password: req.Password, NIM: req.NIM, GroupID: req.GroupID,
		ClientIP: clientAddr(c),
	})
	writeTicket(c, t, err)
}

// Forgot menangani POST /api/v1/auth/password/forgot: kirim kode reset password.
func (h *AuthHandler) Forgot(c *gin.Context) {
	var req forgotRequest
	if !httpx.DecodeJSON(c, &req) || !verifyHuman(c, h.turnstile, req.TurnstileToken) {
		return
	}
	t, err := h.otp.Forgot(c.Request.Context(), req.Phone, clientAddr(c))
	writeTicket(c, t, err)
}

// Resend menangani POST /api/v1/auth/otp/resend: kode baru untuk challenge yang sama tujuannya.
func (h *AuthHandler) Resend(c *gin.Context) {
	var req resendRequest
	if !httpx.DecodeJSON(c, &req) || !verifyHuman(c, h.turnstile, req.TurnstileToken) {
		return
	}
	t, err := h.otp.Resend(c.Request.Context(), req.ChallengeID, clientAddr(c))
	writeTicket(c, t, err)
}

// Login menangani POST /api/v1/auth/login: nomor HP + password.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if !httpx.DecodeJSON(c, &req) {
		return
	}
	res, err := h.login.Login(c.Request.Context(), req.Phone, req.Password, clientAddr(c))
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	writeLogin(c, res)
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
	writeOTPStatus(c, h.otp.Status)
}

func writeOTPStatus(c *gin.Context, status func(context.Context, uuid.UUID) (*usecase.OTPStatusView, error)) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		id = uuid.Nil
	}
	v, err := status(c.Request.Context(), id)
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
	NewPassword string    `json:"new_password"` // wajib untuk reset password
}

type userDTO struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Phone string    `json:"phone"` // disamarkan
	Role  string    `json:"role"`
	NIM   *string   `json:"nim"`   // null untuk akun lama
	Group *groupDTO `json:"group"` // null bila belum punya kelompok
}

// userGroup: kelompok pengguna sebagai {id, name}, atau nil.
func userGroup(u *domain.User) *groupDTO {
	if u.GroupID == nil {
		return nil
	}
	return &groupDTO{ID: *u.GroupID, Name: u.GroupName}
}

func toUserDTO(u *domain.User) userDTO {
	return userDTO{ID: u.ID, Name: u.Name, Email: u.Email, Phone: domain.MaskPhone(u.Phone), Role: "user",
		NIM: optString(u.NIM), Group: userGroup(u)}
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
	res, err := h.verifier.Verify(c.Request.Context(), usecase.VerifyInput{
		ChallengeID: req.ChallengeID, Code: req.Code, NewPassword: req.NewPassword, ClientIP: clientAddr(c),
	})
	if err != nil {
		httpx.WriteError(c, err)
		return
	}
	writeLogin(c, res)
}

func writeLogin(c *gin.Context, res *usecase.LoginResult) {
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
