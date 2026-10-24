package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
)

// SessionAuthenticator dipenuhi oleh *usecase.Sessions.
type SessionAuthenticator interface {
	Authenticate(ctx context.Context, token string) (*domain.Session, error)
}

// AdminVerifier dipenuhi oleh *usecase.AdminAuth.
type AdminVerifier interface {
	VerifyToken(token string) bool
}

// Access menyatakan siapa yang boleh memakai sebuah rute (spec §9.2).
type Access struct {
	Anonymous bool // tanpa header Authorization (mode none; kepemilikan lewat edit_token)
	User      bool // sesi pengguna (mode otp)
	Admin     bool // token admin (hanya baca)
}

// Authenticator memeriksa header Authorization: token admin dikenali lebih dulu, lalu sesi
// pengguna (bila mode otp). sessions/admin boleh nil (mode none / admin dimatikan).
type Authenticator struct {
	sessions SessionAuthenticator
	admin    AdminVerifier
}

func NewAuthenticator(sessions SessionAuthenticator, admin AdminVerifier) *Authenticator {
	return &Authenticator{sessions: sessions, admin: admin}
}

// bearerToken mengambil token dari header "Authorization: Bearer <token>". present false bila
// header tidak ada; ok false bila ada tetapi formatnya salah.
func bearerToken(c *gin.Context) (token string, present, ok bool) {
	h := c.GetHeader("Authorization")
	if h == "" {
		return "", false, false
	}
	scheme, tok, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(tok) == "" {
		return "", true, false
	}
	return strings.TrimSpace(tok), true, true
}

func unauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer realm="photo-annotated"`)
	httpx.AbortError(c, http.StatusUnauthorized, "UNAUTHORIZED", "missing, invalid, or expired session")
}

// Require menegakkan akses rute. Token yang ada tetapi tidak sah selalu 401 (FE tahu harus login
// ulang); admin pada rute yang mengubah data → 403 ADMIN_READ_ONLY.
func (a *Authenticator) Require(acc Access) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, present, ok := bearerToken(c)
		switch {
		case !present:
			if !acc.Anonymous {
				unauthorized(c)
				return
			}
			c.Next()
			return
		case !ok:
			unauthorized(c)
			return
		}
		if a.admin != nil && a.admin.VerifyToken(token) {
			if !acc.Admin {
				httpx.AbortError(c, http.StatusForbidden, "ADMIN_READ_ONLY", "the admin account can only read and export")
				return
			}
			httpx.SetAdmin(c)
			c.Next()
			return
		}
		if a.sessions == nil || !acc.User {
			unauthorized(c)
			return
		}
		s, err := a.sessions.Authenticate(c.Request.Context(), token)
		if err != nil {
			if de, ok := domain.AsError(err); ok && de.Kind == domain.KindUnauthorized {
				unauthorized(c)
			} else {
				httpx.WriteError(c, err)
			}
			return
		}
		httpx.SetSession(c, s.UserID, s.ID)
		c.Next()
	}
}
