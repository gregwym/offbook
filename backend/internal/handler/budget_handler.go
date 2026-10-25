package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type BudgetHandler struct {
	svc *service.BudgetService
}

// personalOwner is the session user's personal scope. Budgets created through
// this handler are always personal; household budgets go through the household
// handler (ADR-0018).
func personalOwner(c *gin.Context) repository.PlanOwner {
	return repository.UserOwner(auth.MustUserID(c.Request.Context()))
}

func NewBudgetHandler(s *service.BudgetService) *BudgetHandler {
	return &BudgetHandler{svc: s}
}

func (h *BudgetHandler) Register(g *gin.RouterGroup) {
	g.POST("/budgets", h.Create)
	g.GET("/budgets", h.List)
	g.GET("/budgets/summary", h.Summary)
	g.GET("/budgets/:id", h.Get)
	g.GET("/budgets/:id/spend", h.Spend)
	g.PATCH("/budgets/:id", h.Update)
	g.DELETE("/budgets/:id", h.Delete)
}

type createBudgetRequest struct {
	CategoryID    int64            `json:"category_id"`
	Period        string           `json:"period"`
	Amount        decimal.Decimal  `json:"amount"`
	Rollover      *bool            `json:"rollover"`
	RolloverStart *decimal.Decimal `json:"rollover_start"`
	IsActive      *bool            `json:"is_active"`
}

func (h *BudgetHandler) Create(c *gin.Context) {
	var req createBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	b, err := h.svc.Create(c.Request.Context(), personalOwner(c), service.CreateBudgetInput{
		CategoryID:    req.CategoryID,
		Period:        req.Period,
		Amount:        req.Amount,
		Rollover:      req.Rollover,
		RolloverStart: req.RolloverStart,
		IsActive:      req.IsActive,
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
	b, err := h.svc.Get(c.Request.Context(), personalOwner(c), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *BudgetHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), personalOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

type updateBudgetRequest struct {
	CategoryID    *int64           `json:"category_id"`
	Period        *string          `json:"period"`
	Amount        *decimal.Decimal `json:"amount"`
	Rollover      *bool            `json:"rollover"`
	RolloverStart *decimal.Decimal `json:"rollover_start"`
	IsActive      *bool            `json:"is_active"`
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
	b, err := h.svc.Update(c.Request.Context(), personalOwner(c), id, service.UpdateBudgetInput(req))
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
	if err := h.svc.SoftDelete(c.Request.Context(), personalOwner(c), id); err != nil {
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
	userID := auth.MustUserID(c.Request.Context())
	var (
		view *service.BudgetSpend
		err  error
	)
	// ?on=YYYY-MM-DD picks the period containing that date (#577); absent
	// means the current period.
	if raw := c.Query("on"); raw != "" {
		on, perr := time.Parse("2006-01-02", raw)
		if perr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "on must be a YYYY-MM-DD date", "code": "INVALID_DATE"})
			return
		}
		view, err = h.svc.SpendOn(c.Request.Context(), userID, id, on)
	} else {
		view, err = h.svc.Spend(c.Request.Context(), userID, id)
	}
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

// Summary is the month's budget totals and unbudgeted spending (#589).
// ?month=YYYY-MM picks a past month; absent means the current month.
func (h *BudgetHandler) Summary(c *gin.Context) {
	on := time.Now().UTC()
	if raw := c.Query("month"); raw != "" {
		m, err := time.Parse("2006-01", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "month must be YYYY-MM", "code": "INVALID_DATE"})
			return
		}
		// The month's last day, or today while the month is still running.
		// A future month keeps its first day, which the service refuses.
		if last := m.AddDate(0, 1, -1); m.After(on) {
			on = m
		} else if last.Before(on) {
			on = last
		}
	}
	view, err := h.svc.MonthSummary(c.Request.Context(), auth.MustUserID(c.Request.Context()), on)
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
	case errors.Is(err, service.ErrBudgetDateInFuture):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_DATE"})
	case errors.Is(err, service.ErrInvalidRolloverStart):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_ROLLOVER_START"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
