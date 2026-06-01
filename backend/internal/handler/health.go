package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/build"
	"github.com/gregwym/offbook/backend/internal/db"
)

type HealthHandler struct {
	db *gorm.DB
}

func NewHealthHandler(g *gorm.DB) *HealthHandler {
	return &HealthHandler{db: g}
}

// Register wires the public health route. It lives on the handler (rather
// than inline in router.go) so contract-check discovers GET /health when the
// frontend calls it for the build-version readout (#310).
func (h *HealthHandler) Register(g *gin.RouterGroup) {
	g.GET("/health", h.Get)
}

func (h *HealthHandler) Get(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if h.db != nil {
		if err := db.Ping(ctx, h.db); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"data":  gin.H{"status": "down", "db": err.Error(), "version": build.Version},
				"error": "database unreachable",
				"code":  "DB_UNAVAILABLE",
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"status": "ok", "version": build.Version}})
}
