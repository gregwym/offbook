package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// TransferHandler serves the #371 transfer-matching surface: proposed
// candidate pairs, confirming or dismissing a match, and unlinking one.
type TransferHandler struct {
	svc *service.TransferMatchingService
}

func NewTransferHandler(s *service.TransferMatchingService) *TransferHandler {
	return &TransferHandler{svc: s}
}

func (h *TransferHandler) Register(g *gin.RouterGroup) {
	g.GET("/transfers/candidates", h.Candidates)
	g.POST("/transfers/confirm", h.Confirm)
	g.POST("/transfers/dismiss", h.Dismiss)
	g.POST("/transactions/:id/unlink-transfer", h.Unlink)
}

// transferCandidateWire is the wire shape for one proposed pair.
type transferCandidateWire struct {
	TransactionA any `json:"transaction_a"`
	TransactionB any `json:"transaction_b"`
}

// Candidates returns the current set of proposed transfer-match pairs for
// the signed-in user. Pure read — nothing is linked until Confirm is called.
func (h *TransferHandler) Candidates(c *gin.Context) {
	candidates, err := h.svc.ProposeMatches(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	wire := make([]transferCandidateWire, 0, len(candidates))
	for _, cand := range candidates {
		wire = append(wire, transferCandidateWire{TransactionA: cand.A, TransactionB: cand.B})
	}
	c.JSON(http.StatusOK, gin.H{"data": wire, "total": int64(len(wire))})
}

type transferPairRequest struct {
	TransactionIDA int64 `json:"transaction_id_a"`
	TransactionIDB int64 `json:"transaction_id_b"`
}

// Confirm links two transactions as a matched transfer — the only write
// path in this handler. Nothing auto-links; the user must explicitly POST
// the pair they reviewed in the candidates list.
func (h *TransferHandler) Confirm(c *gin.Context) {
	var req transferPairRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	if req.TransactionIDA <= 0 || req.TransactionIDB <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "transaction_id_a and transaction_id_b are required",
			"code":  "INVALID_REQUEST",
		})
		return
	}
	a, b, err := h.svc.Confirm(c.Request.Context(), auth.MustUserID(c.Request.Context()), req.TransactionIDA, req.TransactionIDB)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": transferCandidateWire{TransactionA: a, TransactionB: b}})
}

// Dismiss marks a proposed pair as "not a transfer" (#510) so it stops being
// proposed and both rows count in income/spending again.
func (h *TransferHandler) Dismiss(c *gin.Context) {
	var req transferPairRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	if req.TransactionIDA <= 0 || req.TransactionIDB <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "transaction_id_a and transaction_id_b are required",
			"code":  "INVALID_REQUEST",
		})
		return
	}
	if err := h.svc.Dismiss(c.Request.Context(), auth.MustUserID(c.Request.Context()), req.TransactionIDA, req.TransactionIDB); err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Unlink restores a confirmed transfer pair to two ordinary transactions.
func (h *TransferHandler) Unlink(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.svc.Unlink(c.Request.Context(), auth.MustUserID(c.Request.Context()), id); err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TransferHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTransactionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "TRANSACTION_NOT_FOUND"})
	case errors.Is(err, service.ErrTransferSameAccount),
		errors.Is(err, service.ErrTransferNotFlow),
		errors.Is(err, service.ErrTransferAlreadyLinked),
		errors.Is(err, service.ErrTransferAmountMismatch):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_TRANSFER_MATCH"})
	case errors.Is(err, repository.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "transaction not found", "code": "TRANSACTION_NOT_FOUND"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
