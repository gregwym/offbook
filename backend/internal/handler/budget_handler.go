package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type BudgetHandler struct {
	svc *service.BudgetService
}

func NewBudgetHandler(s *service.BudgetService) *BudgetHandler {
	return &BudgetHandler{svc: s}
}

func (h *BudgetHandler) Register(g *gin.RouterGroup) {
	g.POST("/budgets", h.Create)
	g.GET("/budgets", h.List)
	g.GET("/budgets/:id", h.Get)
	g.GET("/budgets/:id/spend", h.Spend)
	g.PATCH("/budgets/:id", h.Update)
	g.DELETE("/budgets/:id", h.Delete)
}

type createBudgetRequest struct {
	CategoryID int64           `json:"category_id"`
	Period     string          `json:"period"`
	Amount     decimal.Decimal `json:"amount"`
	Rollover   *bool           `json:"rollover"`
	IsActive   *bool           `json:"is_active"`
}

func (h *BudgetHandler) Create(c *gin.Context) {
	var req createBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	b, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), service.CreateBudgetInput{
		CategoryID: req.CategoryID,
		Period:     req.Period,
		Amount:     req.Amount,
		Rollover:   req.Rollover,
		IsActive:   req.IsActive,
	})
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": b})
}

func (h *BudgetHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	b, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *BudgetHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

type updateBudgetRequest struct {
	CategoryID *int64           `json:"category_id"`
	Period     *string          `json:"period"`
	Amount     *decimal.Decimal `json:"amount"`
	Rollover   *bool            `json:"rollover"`
	IsActive   *bool            `json:"is_active"`
}

func (h *BudgetHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	b, err := h.svc.Update(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, service.UpdateBudgetInput(req))
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *BudgetHandler) Delete(c *gin.Context) {
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

func (h *BudgetHandler) Spend(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	view, err := h.svc.Spend(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

func (h *BudgetHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrBudgetNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "BUDGET_NOT_FOUND"})
	case errors.Is(err, service.ErrDuplicateActiveBudget):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "DUPLICATE_ACTIVE_BUDGET"})
	case errors.Is(err, service.ErrUnknownCategory):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "UNKNOWN_CATEGORY"})
	case errors.Is(err, service.ErrInvalidBudgetPeriod):
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   err.Error(),
			"code":    "INVALID_PERIOD",
			"allowed": []string{service.BudgetPeriodMonthly, service.BudgetPeriodWeekly, service.BudgetPeriodAnnual},
		})
	case errors.Is(err, service.ErrInvalidBudgetAmount):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_BUDGET_AMOUNT"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
