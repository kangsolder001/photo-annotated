package domain

import (
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Submission adalah satu kiriman: 1–5 foto beserta anotasinya (spec §4.1).
type Submission struct {
	ID              uuid.UUID
	OwnerID         *uuid.UUID // nil pada auth.mode = none
	ClientRequestID uuid.UUID  // kunci idempotensi dari klien
	ClientIP        *netip.Addr
	Version         int // naik 1 setiap edit (optimistic locking)
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Review          Review
}

// ReviewStatus adalah hasil review admin atas satu submission utuh (spec-labels-admin §5.7).
type ReviewStatus string

const (
	ReviewPending  ReviewStatus = "pending"  // belum direview (default; juga setelah pengguna mengedit)
	ReviewAccepted ReviewStatus = "accepted" // diterima: tidak ikut retensi
	ReviewRejected ReviewStatus = "rejected" // ditolak (alasan wajib)
)

// ParseReviewStatus memvalidasi status dari input admin.
func ParseReviewStatus(s string) (ReviewStatus, error) {
	switch v := ReviewStatus(s); v {
	case ReviewPending, ReviewAccepted, ReviewRejected:
		return v, nil
	}
	return "", Invalid("INVALID_REVIEW_STATUS", "status must be pending, accepted, or rejected")
}

// Review adalah status review submission. Status kosong dibaca sebagai pending.
type Review struct {
	Status     ReviewStatus
	Reason     string     // wajib untuk rejected, kosong untuk pending
	ReviewedAt *time.Time // nil ⇔ pending
}

// NewReview memvalidasi keputusan review: alasan wajib untuk rejected (1–500 karakter), opsional untuk
// accepted, dan harus kosong untuk pending.
func NewReview(status ReviewStatus, reason string, now time.Time) (Review, error) {
	if strings.TrimSpace(reason) == "" {
		reason = ""
	}
	switch status {
	case ReviewPending:
		if reason != "" {
			return Review{}, Invalid("INVALID_REVIEW_REASON", "reason must be empty when resetting a review")
		}
		return Review{Status: ReviewPending}, nil
	case ReviewAccepted, ReviewRejected:
	default:
		return Review{}, Invalid("INVALID_REVIEW_STATUS", "status must be pending, accepted, or rejected")
	}
	r := Review{Status: status, ReviewedAt: &now}
	if reason == "" {
		if status == ReviewRejected {
			return Review{}, Invalid("INVALID_REVIEW_REASON", "a reason is required to reject a submission")
		}
		return r, nil
	}
	s, err := NormalizeReason(reason, "INVALID_REVIEW_REASON")
	if err != nil {
		return Review{}, err
	}
	r.Reason = s
	return r, nil
}

// PhotoWithAnnotations adalah foto beserta anotasinya, dalam urutan tampil.
type PhotoWithAnnotations struct {
	Photo       Photo
	Annotations []Annotation // urutan sesuai pembuatan
}

// SubmissionDetail adalah submission lengkap; Photos berurutan menurut Position dan hanya
// berisi foto berstatus attached.
type SubmissionDetail struct {
	Submission Submission
	Photos     []PhotoWithAnnotations
}
