package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type DashboardHandler struct {
	svc       *service.DashboardService
	budgetSvc *service.BudgetService
}

func NewDashboardHandler(s *service.DashboardService, b *service.BudgetService) *DashboardHandler {
	return &DashboardHandler{svc: s, budgetSvc: b}
}

func (h *DashboardHandler) Register(g *gin.RouterGroup) {
	g.GET("/dashboard/summary", h.Summary)
	g.GET("/dashboard/budget-alerts", h.BudgetAlerts)
	g.GET("/dashboard/spend-by-category", h.SpendByCategory)
	g.GET("/dashboard/cash-flow", h.CashFlow)
	g.GET("/dashboard/net-worth", h.NetWorth)
	g.GET("/dashboard/allocation", h.Allocation)
	g.GET("/dashboard/category-trend", h.CategoryTrend)
	g.GET("/dashboard/top-merchants", h.TopMerchants)
}

// Allocation returns the user's positions rolled up by asset kind, valued
// in their primary currency (#341) — the personal counterpart of
// /h/insights/allocation.
func (h *DashboardHandler) Allocation(c *gin.Context) {
	items, err := h.svc.Allocation(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

// SpendByCategory handles ?from=YYYY-MM-DD&to=YYYY-MM-DD. Either bound
// can be omitted to default to the current calendar month.
func (h *DashboardHandler) SpendByCategory(c *gin.Context) {
	from, to, ok := readFromToParams(c)
	if !ok {
		return // handler already responded
	}
	items, err := h.svc.SpendByCategory(c.Request.Context(), auth.MustUserID(c.Request.Context()), from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

// readFromToParams parses the shared ?from=YYYY-MM-DD&to=YYYY-MM-DD query
// params. Either bound may be omitted (zero time.Time — callers default to
// the current calendar month). Writes a 400 response and returns ok=false
// on a malformed date.
func readFromToParams(c *gin.Context) (from, to time.Time, ok bool) {
	if v := c.Query("from"); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "from must be YYYY-MM-DD", "code": "INVALID_REQUEST"})
			return time.Time{}, time.Time{}, false
		}
		from = d
	}
	if v := c.Query("to"); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to must be YYYY-MM-DD", "code": "INVALID_REQUEST"})
			return time.Time{}, time.Time{}, false
		}
		to = d
	}
	return from, to, true
}

// CategoryTrend handles ?months=6 (default 6, capped at 36) — the
// month-over-month category spending trend (#367).
func (h *DashboardHandler) CategoryTrend(c *gin.Context) {
	months := readMonthsParam(c, 6, 36)
	if months < 0 {
		return
	}
	items, err := h.svc.CategoryTrend(c.Request.Context(), auth.MustUserID(c.Request.Context()), months)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

// TopMerchants handles ?from=YYYY-MM-DD&to=YYYY-MM-DD&limit=10. Bounds
// default to the current calendar month; limit defaults to 10, capped at 50.
func (h *DashboardHandler) TopMerchants(c *gin.Context) {
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
	items, err := h.svc.TopMerchants(c.Request.Context(), auth.MustUserID(c.Request.Context()), from, to, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

// CashFlow handles ?months=12. Cap to a sane upper bound to keep the
// query bounded.
func (h *DashboardHandler) CashFlow(c *gin.Context) {
	months := readMonthsParam(c, 12, 36)
	if months < 0 {
		return // handler already responded
	}
	items, err := h.svc.CashFlow(c.Request.Context(), auth.MustUserID(c.Request.Context()), months)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

// NetWorth handles ?months=12 (same cap as CashFlow).
func (h *DashboardHandler) NetWorth(c *gin.Context) {
	months := readMonthsParam(c, 12, 36)
	if months < 0 {
		return
	}
	items, err := h.svc.NetWorth(c.Request.Context(), auth.MustUserID(c.Request.Context()), months)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": int64(len(items))})
}

func readMonthsParam(c *gin.Context, def, max int) int {
	v := c.Query("months")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "months must be a positive integer", "code": "INVALID_REQUEST"})
		return -1
	}
	if n > max {
		n = max
	}
	return n
}

// BudgetAlerts returns the user's active budgets at ≥80% spend for the
// current period (warning) or ≥100% (over). Empty list when nothing
// trips the thresholds.
func (h *DashboardHandler) BudgetAlerts(c *gin.Context) {
	alerts, err := h.budgetSvc.Alerts(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": alerts, "total": int64(len(alerts))})
}

// Summary handles GET /dashboard/summary.
//
// Query params:
//
//	period — one of "current_month" (default), "last_30d", "ytd".
//
// Date semantics: the returned "from" is inclusive, "to" is exclusive.
// All monetary fields are decimal strings; the frontend's shared
// AmountDisplay component is the only place these get formatted.
func (h *DashboardHandler) Summary(c *gin.Context) {
	period := c.DefaultQuery("period", service.PeriodCurrentMonth)
	summary, err := h.svc.Summarize(c.Request.Context(), auth.MustUserID(c.Request.Context()), period)
	if err != nil {
		if errors.Is(err, service.ErrInvalidPeriod) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   err.Error(),
				"code":    "INVALID_PERIOD",
				"allowed": []string{service.PeriodCurrentMonth, service.PeriodLast30D, service.PeriodYTD},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": summary})
}
