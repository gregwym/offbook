package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/prices"
)

// PriceHandler exposes the manual price refresh (ADR-0014 Phase 1). The
// refresh is user-initiated by design: clicking it is the egress consent —
// only the user's held symbols leave the box, never quantities or PII.
type PriceHandler struct {
	svc *prices.Service
}

func NewPriceHandler(s *prices.Service) *PriceHandler {
	return &PriceHandler{svc: s}
}

func (h *PriceHandler) Register(g *gin.RouterGroup) {
	g.POST("/prices/refresh", h.Refresh)
}

func (h *PriceHandler) Refresh(c *gin.Context) {
	result, err := h.svc.RefreshForUser(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		// Upstream provider failures surface as 502: the request was fine,
		// the price source wasn't. Valuations keep their stale flags.
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "code": "PRICE_PROVIDER_ERROR"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
