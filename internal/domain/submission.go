package domain

import (
	"net/netip"
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
