package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// AICategorizationVerdictHandler exposes the read-only AI merchant→category
// verdict cache — the source list for the frontend's "promote to rule"
// affordance (ADR-0022 §6).
type AICategorizationVerdictHandler struct {
	svc *service.AICategorizationVerdictService
}

func NewAICategorizationVerdictHandler(s *service.AICategorizationVerdictService) *AICategorizationVerdictHandler {
	return &AICategorizationVerdictHandler{svc: s}
}

func (h *AICategorizationVerdictHandler) Register(g *gin.RouterGroup) {
	g.GET("/categorization-verdicts", h.List)
}

func (h *AICategorizationVerdictHandler) List(c *gin.Context) {
	uid := auth.MustUserID(c.Request.Context())
	verdicts, err := h.svc.List(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": verdicts, "total": int64(len(verdicts))})
}
