package domain

import (
	"net/netip"
	"time"

	"github.com/google/uuid"
)

type OTPStatus string

const (
	OTPQueued   OTPStatus = "queued"   // menunggu giliran dikirim
	OTPSending  OTPStatus = "sending"  // sedang dikirim worker
	OTPSent     OTPStatus = "sent"     // terkirim; kode bisa diverifikasi sampai expires_at
	OTPFailed   OTPStatus = "failed"   // gagal dikirim (lihat FailureReason)
	OTPConsumed OTPStatus = "consumed" // sudah dipakai
	OTPExpired  OTPStatus = "expired"  // kedaluwarsa atau digantikan challenge baru
)

// Active berarti challenge masih berjalan (belum final): hanya satu per nomor.
func (s OTPStatus) Active() bool { return s == OTPQueued || s == OTPSending || s == OTPSent }

// OTPPurpose: untuk apa kode dipakai. OTP hanya untuk membuktikan kepemilikan nomor saat registrasi
// dan saat reset password; login biasa memakai nomor HP + password.
type OTPPurpose string

const (
	OTPPurposeRegister OTPPurpose = "register"
	OTPPurposeReset    OTPPurpose = "reset"
)

// Alasan kegagalan kirim yang boleh dilihat klien (spec §5.8).
const (
	OTPFailWAUnavailable = "WA_UNAVAILABLE" // gateway tidak terhubung, timeout, atau token ditolak
	OTPFailSendFailed    = "SEND_FAILED"    // gateway menolak pengiriman
)

// OTPChallenge adalah satu permintaan kode verifikasi. Kode asli tidak pernah disimpan, hanya
// HMAC-nya.
type OTPChallenge struct {
	ID      uuid.UUID
	UserID  uuid.UUID
	Phone   string // salinan nomor kanonik saat dibuat
	Purpose OTPPurpose
	// Data registrasi (hanya Purpose = register). Disimpan di challenge, bukan di pengguna, dan baru
	// diterapkan saat kode challenge INI diverifikasi: pendaftaran ulang nomor yang sama oleh orang
	// lain tidak bisa menyisipkan password ke akun pemilik nomor.
	RegName, RegEmail, RegPasswordHash string
	CodeHash                           []byte
	Status                             OTPStatus
	FailureReason                      string
	VerifyAttempts                     int
	SendAttempts                       int
	NextAttemptAt                      time.Time
	WAMessageID                        string
	ClientIP                           *netip.Addr
	CreatedAt                          time.Time
	SentAt                             *time.Time
	ExpiresAt                          *time.Time // sent_at + TTL; nil selama belum terkirim
}

// Session adalah sesi login (token bearer). Token asli hanya dikirim sekali ke klien; di sini
// hanya hash SHA-256-nya.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	ClientIP  *netip.Addr
}

// Active: belum dicabut dan belum kedaluwarsa pada waktu now.
func (s *Session) Active(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}
