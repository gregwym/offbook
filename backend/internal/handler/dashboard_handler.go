package handler

import (
	"errors"
	"net/http"

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
