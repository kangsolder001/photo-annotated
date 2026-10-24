package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

const (
	corsAllowMethods = "GET, POST, PUT, OPTIONS"
	corsMaxAge       = "3600"
	// Header yang boleh dikirim browser (spec §9.4).
	corsAllowHeaders = "Content-Type, Authorization, X-Edit-Token"
	// Header respons yang boleh dibaca FE (Retry-After dipakai untuk backoff upload).
	corsExposeHeaders = "X-Request-ID, Retry-After"
)

var (
	corsMethodSet = toSet("GET", "POST", "PUT", "OPTIONS")
	corsHeaderSet = toSet("content-type", "authorization", "x-edit-token")
)

// CORS menangani preflight dan menambahkan header CORS untuk origin yang diizinkan.
// Dipasang global (sebelum routing) agar preflight untuk rute apa pun, termasuk rute
// yang hanya mendefinisikan PUT, dijawab tanpa menyentuh auth/rate limit.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next() // bukan request lintas-origin dari browser
			return
		}
		_, ok := allowed[origin]
		c.Writer.Header().Add("Vary", "Origin")

		isPreflight := c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != ""
		if isPreflight {
			c.Writer.Header().Add("Vary", "Access-Control-Request-Method")
			c.Writer.Header().Add("Vary", "Access-Control-Request-Headers")
			if !ok {
				httpx.AbortError(c, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "origin not allowed")
				return
			}
			if _, mok := corsMethodSet[strings.ToUpper(c.GetHeader("Access-Control-Request-Method"))]; !mok {
				httpx.AbortError(c, http.StatusForbidden, "CORS_METHOD_NOT_ALLOWED", "method not allowed by CORS policy")
				return
			}
			for _, h := range strings.Split(c.GetHeader("Access-Control-Request-Headers"), ",") {
				h = strings.ToLower(strings.TrimSpace(h))
				if h == "" {
					continue
				}
				if _, hok := corsHeaderSet[h]; !hok {
					httpx.AbortError(c, http.StatusForbidden, "CORS_HEADER_NOT_ALLOWED", "header not allowed by CORS policy")
					return
				}
			}
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", corsAllowMethods)
			h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
			h.Set("Access-Control-Max-Age", corsMaxAge)
			c.Status(http.StatusNoContent)
			c.Abort()
			c.Writer.WriteHeaderNow()
			return
		}

		if ok {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
		}
		// Origin tak dikenal pada request biasa tidak diblokir di server (CORS bukan mekanisme
		// otorisasi); browser menolak membaca respons karena tidak ada header izin.
		c.Next()
	}
}

func toSet(items ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, i := range items {
		m[i] = struct{}{}
	}
	return m
}
