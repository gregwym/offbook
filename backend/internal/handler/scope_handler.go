package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// ScopeHandler exposes GET/PATCH /me/scope. Both routes require a valid
// session; mounted under the secured group in router.New.
type ScopeHandler struct {
	svc *service.ScopeService
}

func NewScopeHandler(s *service.ScopeService) *ScopeHandler {
	return &ScopeHandler{svc: s}
}

func (h *ScopeHandler) Register(g *gin.RouterGroup) {
	g.GET("/me/scope", h.Get)
	g.PATCH("/me/scope", h.Patch)
}

func (h *ScopeHandler) Get(c *gin.Context) {
	view, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

type patchScopeRequest struct {
	Scope string `json:"scope"`
}

func (h *ScopeHandler) Patch(c *gin.Context) {
	var req patchScopeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	view, err := h.svc.Set(c.Request.Context(), auth.MustUserID(c.Request.Context()), req.Scope)
	if err != nil {
		if errors.Is(err, service.ErrInvalidScope) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_SCOPE"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}
