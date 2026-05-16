package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service"
)

type TransactionHandler struct {
	svc *service.TransactionService
}

func NewTransactionHandler(s *service.TransactionService) *TransactionHandler {
	return &TransactionHandler{svc: s}
}

func (h *TransactionHandler) Register(g *gin.RouterGroup) {
	g.POST("/transactions", h.Create)
	g.GET("/transactions/:id", h.Get)
	g.PATCH("/transactions/:id", h.Update)
	g.DELETE("/transactions/:id", h.Delete)
}

// JSON shape for POST /transactions.
// transaction_date accepts either RFC3339 ("2026-05-16T00:00:00Z") or
// a plain date ("2026-05-16"). We decode as string and parse explicitly so
// we can reject ambiguous input early.
type createTransactionRequest struct {
	AccountID       int64            `json:"account_id"`
	CategoryID      *int64           `json:"category_id"`
	Amount          *decimal.Decimal `json:"amount"`
	Currency        string           `json:"currency"`
	Description     *string          `json:"description"`
	MerchantName    *string          `json:"merchant_name"`
	TransactionDate string           `json:"transaction_date"`
	PostedDate      *string          `json:"posted_date"`
	Source          string           `json:"source"`
	Notes           *string          `json:"notes"`
}

func (h *TransactionHandler) Create(c *gin.Context) {
	var req createTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	if req.Amount == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount is required", "code": "INVALID_REQUEST"})
		return
	}
	txDate, ok := parseFlexibleDate(c, req.TransactionDate, "transaction_date")
	if !ok {
		return
	}
	in := service.CreateTransactionInput{
		AccountID:       req.AccountID,
		CategoryID:      req.CategoryID,
		Amount:          *req.Amount,
		Currency:        req.Currency,
		Description:     req.Description,
		MerchantName:    req.MerchantName,
		TransactionDate: txDate,
		Source:          req.Source,
		Notes:           req.Notes,
	}
	if req.PostedDate != nil {
		pd, ok := parseFlexibleDate(c, *req.PostedDate, "posted_date")
		if !ok {
			return
		}
		in.PostedDate = &pd
	}
	t, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": t})
}

func (h *TransactionHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	t, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// updateTransactionRequest mirrors the sparse-patch service input. A null
// category_id with clear_category=true uncategorizes; sending a non-null
// category_id sets it. clear_category=false (default) + null category_id = leave alone.
type updateTransactionRequest struct {
	CategoryID      *int64           `json:"category_id"`
	ClearCategory   bool             `json:"clear_category"`
	Amount          *decimal.Decimal `json:"amount"`
	Currency        *string          `json:"currency"`
	Description     *string          `json:"description"`
	MerchantName    *string          `json:"merchant_name"`
	TransactionDate *string          `json:"transaction_date"`
	PostedDate      *string          `json:"posted_date"`
	Notes           *string          `json:"notes"`
}

func (h *TransactionHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateTransactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	in := service.UpdateTransactionInput{
		CategoryID:    req.CategoryID,
		ClearCategory: req.ClearCategory,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Description:   req.Description,
		MerchantName:  req.MerchantName,
		Notes:         req.Notes,
	}
	if req.TransactionDate != nil {
		td, ok := parseFlexibleDate(c, *req.TransactionDate, "transaction_date")
		if !ok {
			return
		}
		in.TransactionDate = &td
	}
	if req.PostedDate != nil {
		pd, ok := parseFlexibleDate(c, *req.PostedDate, "posted_date")
		if !ok {
			return
		}
		in.PostedDate = &pd
	}
	t, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *TransactionHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.svc.SoftDelete(c.Request.Context(), id); err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TransactionHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTransactionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "TRANSACTION_NOT_FOUND"})
	case errors.Is(err, service.ErrAccountNotFound):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "ACCOUNT_NOT_FOUND"})
	case errors.Is(err, service.ErrInvalidCategory):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_CATEGORY"})
	case errors.Is(err, service.ErrInvalidAmount),
		errors.Is(err, service.ErrInvalidSource),
		errors.Is(err, service.ErrMissingDate),
		errors.Is(err, service.ErrInvalidCurrency):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_TRANSACTION"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}

// parseFlexibleDate accepts "2026-05-16" or RFC3339. On failure it writes a
// 400 response and returns ok=false so callers can early-return.
func parseFlexibleDate(c *gin.Context, raw, field string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	c.JSON(http.StatusBadRequest, gin.H{
		"error": field + " must be YYYY-MM-DD or RFC3339",
		"code":  "INVALID_REQUEST",
	})
	return time.Time{}, false
}
