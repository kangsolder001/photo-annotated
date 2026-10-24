package usecase

import (
	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// Caller adalah kredensial yang disajikan sebuah request. Middleware autentikasi mengisinya;
// usecase memutuskan apa yang boleh dilakukan (spec §9.2).
type Caller struct {
	EditToken string     // header X-Edit-Token (auth.mode = none)
	UserID    *uuid.UUID // pengguna terautentikasi (auth.mode = otp)
	Admin     bool       // admin (login statis)
}

type authorizer struct {
	editTokens EditTokenSigner // nil bila tidak dipakai
}

// canRead: pemegang token edit yang sah, pemilik (user), atau admin.
func (a authorizer) canRead(c Caller, s *domain.Submission) bool {
	if c.Admin {
		return true
	}
	if a.editTokens != nil && c.EditToken != "" && a.editTokens.Verify(s.ID, c.EditToken) {
		return true
	}
	return c.UserID != nil && s.OwnerID != nil && *c.UserID == *s.OwnerID
}

// canEdit: hanya pemilik. Admin hanya boleh membaca (spec §4.4).
func (a authorizer) canEdit(c Caller, s *domain.Submission) bool {
	if a.editTokens != nil && c.EditToken != "" && a.editTokens.Verify(s.ID, c.EditToken) {
		return true
	}
	return c.UserID != nil && s.OwnerID != nil && *c.UserID == *s.OwnerID
}
