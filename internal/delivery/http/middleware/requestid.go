// Package middleware berisi middleware Gin: request id, IP klien, logging, recovery, deadline.
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

const HeaderRequestID = "X-Request-ID"

// Request id dari klien hanya diterima bila berbentuk aman (cegah log injection).
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestID memastikan setiap request punya id, dan mengembalikannya di header respons.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if !safeRequestID.MatchString(id) {
			id = newID()
		}
		httpx.SetRequestID(c, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
