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
