// Package handler berisi handler HTTP tipis: bind request, panggil usecase, petakan hasil.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Healthz adalah liveness probe: proses hidup dan bisa melayani request.
func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
