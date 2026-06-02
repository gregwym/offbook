package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

type TransactionHandler struct {
	svc *service.TransactionService
}

func NewTransactionHandler(s *service.TransactionService) *TransactionHandler {
	return &TransactionHandler{svc: s}
}

func (h *TransactionHandler) Register(g *gin.RouterGroup) {
	g.POST("/transactions", h.Create)
	g.GET("/transactions", h.List)
	g.GET("/transactions/:id", h.Get)
	g.PATCH("/transactions/:id", h.Update)
	g.DELETE("/transactions/:id", h.Delete)
	// CSV statement import is account-scoped (#330): the account fixes the
	// asset and the dedup namespace.
	g.POST("/accounts/:id/transactions/import", h.Import)
}

// List returns a paginated, filtered slice of transactions.
//
// Query params:
//
//	account_id   — exact match (positive int64)
//	category_id  — exact match OR the literal "null" to find uncategorized rows
//	from, to     — inclusive date range on transaction_date, YYYY-MM-DD or RFC3339
//	search       — case-insensitive substring match on description + merchant_name
//	limit        — default 50, clamped to [1, 200]
//	offset       — default 0, must be non-negative
func (h *TransactionHandler) List(c *gin.Context) {
	f := repository.TransactionFilter{}

	if v := c.Query("account_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "account_id must be a positive integer", "code": "INVALID_REQUEST"})
			return
		}
		f.AccountID = &id
	}
	if v := c.Query("category_id"); v != "" {
		if v == "null" {
			f.UncategorizedOnly = true
		} else {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "category_id must be a positive integer or 'null'", "code": "INVALID_REQUEST"})
				return
			}
			f.CategoryID = &id
		}
	}
	if v := c.Query("from"); v != "" {
		t, ok := parseFlexibleDate(c, v, "from")
		if !ok {
			return
		}
		f.From = &t
	}
	if v := c.Query("to"); v != "" {
		t, ok := parseFlexibleDate(c, v, "to")
		if !ok {
			return
		}
		f.To = &t
	}
	if v := c.Query("categorization_method"); v != "" {
		// Only the values the model already constrains are allowed —
		// kept narrow to avoid a surprise filter against arbitrary strings.
		switch v {
		case "plaid_default", "rule", "manual":
			f.CategorizationMethod = v
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "categorization_method must be one of plaid_default, rule, manual",
				"code":  "INVALID_REQUEST",
			})
			return
		}
	}
	f.Search = c.Query("search")
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a non-negative integer", "code": "INVALID_REQUEST"})
			return
		}
		f.Limit = n
	}
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be a non-negative integer", "code": "INVALID_REQUEST"})
			return
		}
		f.Offset = n
	}

	transactions, total, err := h.svc.List(c.Request.Context(), auth.MustUserID(c.Request.Context()), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":   transactions,
		"total":  total,
		"limit":  resolvedLimit(f.Limit),
		"offset": f.Offset,
	})
}

// JSON shape for POST /transactions. Per ADR-0013, the transaction's
// asset is derived server-side from the parent account; the wire payload
// no longer carries a `currency` field.
// transaction_date accepts either RFC3339 ("2026-05-16T00:00:00Z") or
// a plain date ("2026-05-16"). We decode as string and parse explicitly so
// we can reject ambiguous input early.
type createTransactionRequest struct {
	AccountID       int64            `json:"account_id"`
	CategoryID      *int64           `json:"category_id"`
	Amount          *decimal.Decimal `json:"amount"`
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
	t, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), in)
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
	t, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// updateTransactionRequest mirrors the sparse-patch service input. A null
// category_id with clear_category=true uncategorizes; sending a non-null
// category_id sets it. clear_category=false (default) + null category_id = leave alone.
// asset_id is intentionally absent — per ADR-0013, the asset on an existing
// transaction is not user-mutable.
type updateTransactionRequest struct {
	CategoryID      *int64           `json:"category_id"`
	ClearCategory   bool             `json:"clear_category"`
	Amount          *decimal.Decimal `json:"amount"`
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
	t, err := h.svc.Update(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, in)
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
	if err := h.svc.SoftDelete(c.Request.Context(), auth.MustUserID(c.Request.Context()), id); err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Import ingests a CSV statement into the path account. Multipart form fields:
//
//	file            — the CSV file (required)
//	commit          — "true" writes new rows; otherwise (default) preview only
//	date_col        — optional header-name override for the date column
//	amount_col      — optional header-name override for the amount column
//	description_col — optional header-name override for the description column
//
// Omitted *_col fields are auto-detected from common header aliases. The
// response is an ImportResult: per-row new/duplicate/error classification plus
// totals. A preview run writes nothing.
func (h *TransactionHandler) Import(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required", "code": "INVALID_REQUEST"})
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read uploaded file", "code": "INVALID_REQUEST"})
		return
	}
	defer func() { _ = f.Close() }()

	mapping := ingestion.ColumnMapping{
		Date:        c.PostForm("date_col"),
		Amount:      c.PostForm("amount_col"),
		Description: c.PostForm("description_col"),
	}
	parsed, err := ingestion.Parse(f, mapping)
	if err != nil {
		h.writeImportParseError(c, err)
		return
	}
	commit := c.PostForm("commit") == "true"
	res, err := h.svc.ImportCSV(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, parsed, commit)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// writeImportParseError maps ingestion parse failures to 400s with a machine
// code the UI can branch on (e.g. IMPORT_UNMAPPABLE → show column pickers).
func (h *TransactionHandler) writeImportParseError(c *gin.Context, err error) {
	var unmappable *ingestion.UnmappableError
	switch {
	case errors.As(err, &unmappable):
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   unmappable.Error(),
			"code":    "IMPORT_UNMAPPABLE",
			"headers": unmappable.Headers,
			"missing": unmappable.Missing,
		})
	case errors.Is(err, ingestion.ErrEmptyFile), errors.Is(err, ingestion.ErrNoDataRows):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "IMPORT_EMPTY"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "IMPORT_PARSE_ERROR"})
	}
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
