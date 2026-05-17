package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// PIIHandler exposes the deliberate, auditable PII access points.
// Kept in its own file so a reviewer can see every place PII crosses the API
// boundary at a glance.
type PIIHandler struct {
	svc *service.PIIService
}

func NewPIIHandler(s *service.PIIService) *PIIHandler {
	return &PIIHandler{svc: s}
}

// RegisterAccountRoutes mounts the account PII endpoints. These are the ONLY
// way PII leaves the backend — see ARCHITECTURE.md.
func (h *PIIHandler) RegisterAccountRoutes(g *gin.RouterGroup) {
	g.GET("/accounts/:id/pii", h.GetForAccount)
	g.PUT("/accounts/:id/pii", h.SetForAccount)
}

func (h *PIIHandler) GetForAccount(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	uid := auth.MustUserID(c.Request.Context())
	pii, err := h.svc.GetAccountPII(c.Request.Context(), uid, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pii})
}

func (h *PIIHandler) SetForAccount(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	if len(req) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "body must be a non-empty object of {field_name: value}",
			"code":    "INVALID_REQUEST",
			"allowed": service.AllowedAccountPIIFields(),
		})
		return
	}
	uid := auth.MustUserID(c.Request.Context())
	if err := h.svc.SetAccountPII(c.Request.Context(), uid, id, req); err != nil {
		h.writeError(c, err)
		return
	}
	// Read back so the client sees the full current state after the upsert.
	pii, err := h.svc.GetAccountPII(c.Request.Context(), uid, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pii})
}

func (h *PIIHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "ACCOUNT_NOT_FOUND"})
	case errors.Is(err, service.ErrInvalidPIIField):
		// Include the allowlist so the caller can fix the request without guessing.
		msg := err.Error()
		// Strip the "invalid pii field: " prefix for the user-facing message? Keep as-is —
		// the wrapped field name is useful and the code disambiguates programmatically.
		_ = strings.TrimSpace(msg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   msg,
			"code":    "INVALID_PII_FIELD",
			"allowed": service.AllowedAccountPIIFields(),
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
