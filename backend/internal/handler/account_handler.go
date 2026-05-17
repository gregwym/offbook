package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type AccountHandler struct {
	svc *service.AccountService
}

func NewAccountHandler(s *service.AccountService) *AccountHandler {
	return &AccountHandler{svc: s}
}

// Register attaches account routes to the given /api/v1 group.
func (h *AccountHandler) Register(g *gin.RouterGroup) {
	g.POST("/accounts", h.Create)
	g.GET("/accounts", h.List)
	g.GET("/accounts/:id", h.Get)
	g.PATCH("/accounts/:id", h.Update)
	g.DELETE("/accounts/:id", h.Delete)
}

// JSON request shape for POST /accounts.
type createAccountRequest struct {
	Name            string           `json:"name"`
	InstitutionSlug string           `json:"institution_slug"`
	AccountType     string           `json:"account_type"`
	Currency        string           `json:"currency"`
	Balance         *decimal.Decimal `json:"balance"`
	LastFour        *string          `json:"last_four"`
	IsActive        *bool            `json:"is_active"`
}

func (h *AccountHandler) Create(c *gin.Context) {
	var req createAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	in := service.CreateAccountInput{
		Name:            req.Name,
		InstitutionSlug: req.InstitutionSlug,
		AccountType:     req.AccountType,
		Currency:        req.Currency,
		LastFour:        req.LastFour,
		IsActive:        req.IsActive,
	}
	if req.Balance != nil {
		in.Balance = *req.Balance
	}
	a, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), in)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": a})
}

func (h *AccountHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	a, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AccountHandler) List(c *gin.Context) {
	f := repository.AccountFilter{
		InstitutionSlug: c.Query("institution_slug"),
		AccountType:     c.Query("account_type"),
	}
	if v := c.Query("is_active"); v != "" {
		switch v {
		case "true":
			t := true
			f.IsActive = &t
		case "false":
			fv := false
			f.IsActive = &fv
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "is_active must be true or false", "code": "INVALID_REQUEST"})
			return
		}
	}
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

	accounts, total, err := h.svc.List(c.Request.Context(), auth.MustUserID(c.Request.Context()), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":   accounts,
		"total":  total,
		"limit":  resolvedLimit(f.Limit),
		"offset": f.Offset,
	})
}

type updateAccountRequest struct {
	Name            *string          `json:"name"`
	InstitutionSlug *string          `json:"institution_slug"`
	AccountType     *string          `json:"account_type"`
	Currency        *string          `json:"currency"`
	Balance         *decimal.Decimal `json:"balance"`
	LastFour        *string          `json:"last_four"`
	IsActive        *bool            `json:"is_active"`
}

func (h *AccountHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	a, err := h.svc.Update(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, service.UpdateAccountInput(req))
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}

func (h *AccountHandler) Delete(c *gin.Context) {
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

func (h *AccountHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "ACCOUNT_NOT_FOUND"})
	case errors.Is(err, service.ErrEmptyName),
		errors.Is(err, service.ErrEmptyInstitution),
		errors.Is(err, service.ErrInvalidType),
		errors.Is(err, service.ErrInvalidCurrency),
		errors.Is(err, service.ErrInvalidLastFour),
		errors.Is(err, service.ErrInvalidAccount):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_ACCOUNT"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id must be a positive integer", "code": "INVALID_REQUEST"})
		return 0, false
	}
	return id, true
}

func resolvedLimit(requested int) int {
	if requested <= 0 {
		return 50
	}
	if requested > 200 {
		return 200
	}
	return requested
}
