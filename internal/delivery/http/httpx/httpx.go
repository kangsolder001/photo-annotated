// Package httpx berisi helper kecil yang dipakai bersama oleh handler dan middleware:
// format error JSON (spec §5) dan akses nilai per-request di gin.Context.
package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	keyRequestID = "request_id"
	keyClientIP  = "client_ip"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Error menulis respons error berformat standar.
func Error(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// AbortError menulis error lalu menghentikan rantai handler.
func AbortError(c *gin.Context, status int, code, message string) {
	c.Abort()
	Error(c, status, code, message)
}

func SetRequestID(c *gin.Context, id string) { c.Set(keyRequestID, id) }

func RequestID(c *gin.Context) string {
	if v, ok := c.Get(keyRequestID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func SetClientIP(c *gin.Context, ip string) { c.Set(keyClientIP, ip) }

func ClientIP(c *gin.Context) string {
	if v, ok := c.Get(keyClientIP); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// NotFound dan MethodNotAllowed dipasang di engine agar semua respons berformat JSON.
func NotFound(c *gin.Context) {
	Error(c, http.StatusNotFound, "NOT_FOUND", "resource not found")
}

func MethodNotAllowed(c *gin.Context) {
	Error(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
}

const (
	keyUserID    = "auth_user_id"
	keySessionID = "auth_session_id"
)

// SetSession mencatat pengguna dan sesi yang terautentikasi untuk request ini.
func SetSession(c *gin.Context, userID, sessionID uuid.UUID) {
	c.Set(keyUserID, userID)
	c.Set(keySessionID, sessionID)
}

// Session mengembalikan pengguna dan sesi yang terautentikasi, bila ada.
func Session(c *gin.Context) (userID, sessionID uuid.UUID, ok bool) {
	u, ok1 := c.Get(keyUserID)
	s, ok2 := c.Get(keySessionID)
	if !ok1 || !ok2 {
		return uuid.Nil, uuid.Nil, false
	}
	userID, ok1 = u.(uuid.UUID)
	sessionID, ok2 = s.(uuid.UUID)
	return userID, sessionID, ok1 && ok2
}
