package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// ReconciliationHandler serves the per-account reconciliation view and the
// adjustment-acknowledge action (#370).
type ReconciliationHandler struct {
	svc *service.ReconciliationService
}

func NewReconciliationHandler(s *service.ReconciliationService) *ReconciliationHandler {
	return &ReconciliationHandler{svc: s}
}

// Register attaches reconciliation routes to the given /api/v1 group.
func (h *ReconciliationHandler) Register(g *gin.RouterGroup) {
	g.GET("/accounts/:id/reconciliation", h.Report)
	g.PATCH("/transactions/:id/acknowledge", h.Acknowledge)
}

func (h *ReconciliationHandler) Report(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	report, err := h.svc.Report(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": report})
}

type acknowledgeRequest struct {
	Note *string `json:"note"`
}

func (h *ReconciliationHandler) Acknowledge(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req acknowledgeRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
			return
		}
	}
	tx, err := h.svc.Acknowledge(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, req.Note)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tx})
}

func (h *ReconciliationHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "ACCOUNT_NOT_FOUND"})
	case errors.Is(err, service.ErrTransactionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "TRANSACTION_NOT_FOUND"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
