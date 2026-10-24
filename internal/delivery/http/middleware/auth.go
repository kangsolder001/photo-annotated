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

// authenticate memvalidasi token yang ada; false bila respons 401/error sudah ditulis.
func authenticate(c *gin.Context, a SessionAuthenticator, token string) bool {
	s, err := a.Authenticate(c.Request.Context(), token)
	if err != nil {
		if de, ok := domain.AsError(err); ok && de.Kind == domain.KindUnauthorized {
			unauthorized(c)
		} else {
			httpx.WriteError(c, err)
		}
		return false
	}
	httpx.SetSession(c, s.UserID, s.ID)
	return true
}

// RequireSession mewajibkan sesi yang sah.
func RequireSession(a SessionAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, _, ok := bearerToken(c)
		if !ok {
			unauthorized(c)
			return
		}
		if authenticate(c, a, token) {
			c.Next()
		}
	}
}
