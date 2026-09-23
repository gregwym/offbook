package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// HouseholdAggregatorHandler is the only handler that calls the aggregator.
// It looks up the requester's household via the member repo (read-only) and
// routes the call to the aggregator. No domain repository is touched here.
type HouseholdAggregatorHandler struct {
	agg     *household.Aggregator
	members repository.HouseholdMemberRepository
}

func NewHouseholdAggregatorHandler(agg *household.Aggregator, members repository.HouseholdMemberRepository) *HouseholdAggregatorHandler {
	return &HouseholdAggregatorHandler{agg: agg, members: members}
}

// Register mounts /h/dashboard, /h/budgets/pace, /h/goals/progress,
// /h/ai/context, and the /h/insights/* trio (allocation, net-worth,
// accounts). All are gated by the secured group + the membership lookup
// below (no membership ⇒ 403).
func (h *HouseholdAggregatorHandler) Register(g *gin.RouterGroup) {
	r := g.Group("/h")
	r.GET("/dashboard", h.Dashboard)
	r.GET("/budgets/pace", h.BudgetPace)
	r.GET("/goals/progress", h.GoalProgress)
	r.GET("/ai/context", h.AIContext)
	r.GET("/insights/allocation", h.Allocation)
	r.GET("/insights/net-worth", h.NetWorthTrend)
	r.GET("/insights/accounts", h.AccountSummaries)
	r.GET("/insights/category-trend", h.CategoryTrend)
	r.GET("/insights/top-merchants", h.TopMerchants)
	r.GET("/insights/cash-flow", h.CashFlow)
}

func (h *HouseholdAggregatorHandler) requireHousehold(c *gin.Context) (int64, bool) {
	uid := auth.MustUserID(c.Request.Context())
	mem, err := h.members.GetMembershipForUser(c.Request.Context(), uid)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusForbidden, gin.H{"error": "no household membership", "code": "NO_HOUSEHOLD"})
			return 0, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return 0, false
	}
	if mem.LeftAt != nil {
		// In-grace member — can rejoin via invite, but household routes are gated
		// on active membership.
		c.JSON(http.StatusForbidden, gin.H{"error": "membership inactive", "code": "MEMBERSHIP_INACTIVE"})
		return 0, false
	}
	return mem.HouseholdID, true
}

func (h *HouseholdAggregatorHandler) Dashboard(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", household.PeriodCurrentMonth)
	out, err := h.agg.Dashboard(c.Request.Context(), hhID, period)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HouseholdAggregatorHandler) BudgetPace(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	period := c.DefaultQuery("period", household.PeriodCurrentMonth)
	out, err := h.agg.BudgetPace(c.Request.Context(), hhID, period)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

func (h *HouseholdAggregatorHandler) GoalProgress(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	out, err := h.agg.GoalProgress(c.Request.Context(), hhID)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

func (h *HouseholdAggregatorHandler) AIContext(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	uid := auth.MustUserID(c.Request.Context())
	out, err := h.agg.AIContext(c.Request.Context(), hhID, uid)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HouseholdAggregatorHandler) Allocation(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	out, err := h.agg.Allocation(c.Request.Context(), hhID)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

func (h *HouseholdAggregatorHandler) NetWorthTrend(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	months := 12
	if v := c.Query("months"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 60 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "months must be 1..60", "code": "INVALID_REQUEST"})
			return
		}
		months = n
	}
	out, err := h.agg.NetWorthTrend(c.Request.Context(), hhID, months)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

func (h *HouseholdAggregatorHandler) AccountSummaries(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	out, err := h.agg.AccountSummaries(c.Request.Context(), hhID)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

// CategoryTrend handles ?months=6 (default 6, capped at 36) — the household
// month-over-month category spending trend (#367).
func (h *HouseholdAggregatorHandler) CategoryTrend(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	months := readMonthsParam(c, 6, 36)
	if months < 0 {
		return
	}
	out, err := h.agg.CategoryTrend(c.Request.Context(), hhID, months)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

// TopMerchants handles ?from=YYYY-MM-DD&to=YYYY-MM-DD&limit=10 — the
// household top-merchants view (#367). Bounds default to the current
// calendar month; limit defaults to 10, capped at 50.
func (h *HouseholdAggregatorHandler) TopMerchants(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	from, to, ok := readFromToParams(c)
	if !ok {
		return
	}
	limit := 10
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer", "code": "INVALID_REQUEST"})
			return
		}
		if n > 50 {
			n = 50
		}
		limit = n
	}
	out, err := h.agg.TopMerchants(c.Request.Context(), hhID, from, to, limit)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

// CashFlow handles ?months=6 (default 6, capped at 36) — the household
// income-vs-spending trend by month (#367).
func (h *HouseholdAggregatorHandler) CashFlow(c *gin.Context) {
	hhID, ok := h.requireHousehold(c)
	if !ok {
		return
	}
	months := readMonthsParam(c, 6, 36)
	if months < 0 {
		return
	}
	out, err := h.agg.CashFlow(c.Request.Context(), hhID, months)
	if err != nil {
		writeAggregatorErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

func writeAggregatorErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, household.ErrHouseholdNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "NOT_FOUND"})
	case errors.Is(err, household.ErrInvalidPeriod):
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   err.Error(),
			"code":    "INVALID_PERIOD",
			"allowed": []string{household.PeriodCurrentMonth, household.PeriodLast30D, household.PeriodYTD},
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
