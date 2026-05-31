package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

type SavingsGoalHandler struct {
	svc *service.SavingsGoalService
}

func NewSavingsGoalHandler(s *service.SavingsGoalService) *SavingsGoalHandler {
	return &SavingsGoalHandler{svc: s}
}

func (h *SavingsGoalHandler) Register(g *gin.RouterGroup) {
	g.POST("/savings-goals", h.Create)
	g.GET("/savings-goals", h.List)
	g.GET("/savings-goals/:id", h.Get)
	g.PATCH("/savings-goals/:id", h.Update)
	g.DELETE("/savings-goals/:id", h.Delete)
	g.POST("/savings-goals/:id/contributions", h.Contribute)
}

type createGoalRequest struct {
	Name         string          `json:"name"`
	TargetAmount decimal.Decimal `json:"target_amount"`
	TargetDate   *string         `json:"target_date"` // YYYY-MM-DD
	AccountID    *int64          `json:"account_id"`
}

func (h *SavingsGoalHandler) Create(c *gin.Context) {
	var req createGoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	in := service.CreateGoalInput{
		Name:         req.Name,
		TargetAmount: req.TargetAmount,
		AccountID:    req.AccountID,
	}
	if req.TargetDate != nil && *req.TargetDate != "" {
		d, err := time.Parse("2006-01-02", *req.TargetDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "target_date must be YYYY-MM-DD", "code": "INVALID_REQUEST"})
			return
		}
		in.TargetDate = &d
	}
	g, err := h.svc.Create(c.Request.Context(), personalOwner(c), in)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": viewOrRaw(g)})
}

func (h *SavingsGoalHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	g, err := h.svc.Get(c.Request.Context(), personalOwner(c), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": viewOrRaw(g)})
}

func (h *SavingsGoalHandler) List(c *gin.Context) {
	goals, err := h.svc.List(c.Request.Context(), personalOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	views := make([]service.GoalView, 0, len(goals))
	for i := range goals {
		views = append(views, service.View(&goals[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": views, "total": int64(len(views))})
}

// updateGoalRequest distinguishes "field omitted" (leave alone) from
// "field present with null" (clear). For TargetDate + AccountID the
// frontend sends `null` to clear; the JSON decoder turns that into a
// non-nil pointer-to-pointer pattern via custom checks below.
type updateGoalRequest struct {
	Name         *string          `json:"name"`
	TargetAmount *decimal.Decimal `json:"target_amount"`
	// Use json.RawMessage-like behavior via a separate flag would be
	// cleanest, but a simple pair is fine for two optional clear-fields.
	TargetDate      *string `json:"target_date"`
	ClearTargetDate bool    `json:"clear_target_date"`
	AccountID       *int64  `json:"account_id"`
	ClearAccountID  bool    `json:"clear_account_id"`
}

func (h *SavingsGoalHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateGoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	in := service.UpdateGoalInput{
		Name:            req.Name,
		TargetAmount:    req.TargetAmount,
		ClearTargetDate: req.ClearTargetDate,
		AccountID:       req.AccountID,
		ClearAccountID:  req.ClearAccountID,
	}
	if req.TargetDate != nil && *req.TargetDate != "" {
		d, err := time.Parse("2006-01-02", *req.TargetDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "target_date must be YYYY-MM-DD", "code": "INVALID_REQUEST"})
			return
		}
		in.TargetDate = &d
	}
	g, err := h.svc.Update(c.Request.Context(), personalOwner(c), id, in)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": viewOrRaw(g)})
}

func (h *SavingsGoalHandler) Delete(c *gin.Context) {
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

type contributeRequest struct {
	Amount decimal.Decimal `json:"amount"`
}

func (h *SavingsGoalHandler) Contribute(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req contributeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	g, err := h.svc.Contribute(c.Request.Context(), personalOwner(c), id, req.Amount)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": viewOrRaw(g)})
}

func (h *SavingsGoalHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSavingsGoalNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "SAVINGS_GOAL_NOT_FOUND"})
	case errors.Is(err, service.ErrGoalAccountMismatch):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "ACCOUNT_MISMATCH"})
	case errors.Is(err, service.ErrEmptyGoalName),
		errors.Is(err, service.ErrInvalidTargetAmount),
		errors.Is(err, service.ErrZeroContribution):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_GOAL"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}

func viewOrRaw(g *model.SavingsGoal) service.GoalView {
	return service.View(g)
}
