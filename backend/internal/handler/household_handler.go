package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

type HouseholdHandler struct {
	svc *household.Service
}

func NewHouseholdHandler(s *household.Service) *HouseholdHandler {
	return &HouseholdHandler{svc: s}
}

// Register attaches routes to the authenticated /api/v1 group.
//
//	POST   /households                        create
//	GET    /households/:id                    detail (member-only)
//	PATCH  /households/:id                    update name / grace (owner)
//	DELETE /households/:id                    dissolve (owner)
//	POST   /households/:id/invites            mint invite token (owner)
//	POST   /invites/:token/accept             consume token
//	DELETE /households/:id/members/me         self-leave
//
//	GET    /accounts/:id/shares                       list visibility per household
//	PUT    /accounts/:id/shares/:householdID          set/clear visibility
func (h *HouseholdHandler) Register(g *gin.RouterGroup) {
	g.POST("/households", h.Create)
	g.GET("/households/:id", h.Get)
	g.PATCH("/households/:id", h.Update)
	g.DELETE("/households/:id", h.Dissolve)
	g.POST("/households/:id/invites", h.CreateInvite)
	g.POST("/invites/:token/accept", h.AcceptInvite)
	g.DELETE("/households/:id/members/me", h.LeaveSelf)
	g.GET("/households/:id/members", h.ListMembers)
	g.PATCH("/households/:id/members/:userID", h.UpdateMemberRole)
	g.DELETE("/households/:id/members/:userID", h.RemoveMember)
	g.POST("/households/:id/transfer-owner", h.TransferOwner)
	g.POST("/households/:id/shared-budgets", h.CreateSharedBudget)
	g.GET("/households/:id/shared-budgets", h.ListSharedBudgets)
	g.PATCH("/households/:id/shared-budgets/:budgetID", h.UpdateSharedBudget)
	g.DELETE("/households/:id/shared-budgets/:budgetID", h.DeleteSharedBudget)
	g.POST("/households/:id/shared-goals", h.CreateSharedGoal)
	g.GET("/households/:id/shared-goals", h.ListSharedGoals)
	g.PATCH("/households/:id/shared-goals/:goalID", h.UpdateSharedGoal)
	g.DELETE("/households/:id/shared-goals/:goalID", h.DeleteSharedGoal)
	g.POST("/households/:id/shared-goals/:goalID/contributions", h.ContributeToSharedGoal)

	g.GET("/accounts/:id/shares", h.ListShares)
	g.PUT("/accounts/:id/shares/:householdID", h.SetShare)
}

// --- request shapes ---

type createHouseholdRequest struct {
	Name            string `json:"name"`
	GracePeriodDays *int   `json:"grace_period_days"`
}

type updateHouseholdRequest struct {
	Name            *string `json:"name"`
	GracePeriodDays *int    `json:"grace_period_days"`
}

type createInviteRequest struct {
	Role string `json:"role"`
}

type setShareRequest struct {
	Visibility string `json:"visibility"`
}

type updateMemberRoleRequest struct {
	Role string `json:"role"`
}

type transferOwnerRequest struct {
	UserID int64 `json:"user_id"`
}

type createSharedBudgetRequest struct {
	CategoryID int64           `json:"category_id"`
	Period     string          `json:"period"`
	Amount     decimal.Decimal `json:"amount"`
	Rollover   *bool           `json:"rollover"`
	IsActive   *bool           `json:"is_active"`
}

type updateSharedBudgetRequest struct {
	CategoryID *int64           `json:"category_id"`
	Period     *string          `json:"period"`
	Amount     *decimal.Decimal `json:"amount"`
	Rollover   *bool            `json:"rollover"`
	IsActive   *bool            `json:"is_active"`
}

type createSharedGoalRequest struct {
	Name         string          `json:"name"`
	TargetAmount decimal.Decimal `json:"target_amount"`
	TargetDate   *string         `json:"target_date"` // YYYY-MM-DD
}

type updateSharedGoalRequest struct {
	Name            *string          `json:"name"`
	TargetAmount    *decimal.Decimal `json:"target_amount"`
	TargetDate      *string          `json:"target_date"`
	ClearTargetDate bool             `json:"clear_target_date"`
}

type sharedGoalContributionRequest struct {
	Amount decimal.Decimal `json:"amount"`
}

// --- handlers ---

func (h *HouseholdHandler) Create(c *gin.Context) {
	var req createHouseholdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	hh, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), household.CreateInput{
		Name:            req.Name,
		GracePeriodDays: req.GracePeriodDays,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": hh})
}

func (h *HouseholdHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	detail, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

func (h *HouseholdHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateHouseholdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	hh, err := h.svc.Update(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, household.UpdateInput{
		Name:            req.Name,
		GracePeriodDays: req.GracePeriodDays,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": hh})
}

func (h *HouseholdHandler) Dissolve(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.svc.Dissolve(c.Request.Context(), auth.MustUserID(c.Request.Context()), id); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HouseholdHandler) CreateInvite(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req createInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Empty body is allowed — role defaults to contributor.
		req = createInviteRequest{}
	}
	res, err := h.svc.CreateInvite(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, household.CreateInviteInput{
		Role: req.Role,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": res})
}

func (h *HouseholdHandler) AcceptInvite(c *gin.Context) {
	token := c.Param("token")
	res, err := h.svc.AcceptInvite(c.Request.Context(), auth.MustUserID(c.Request.Context()), token)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func (h *HouseholdHandler) LeaveSelf(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.svc.Leave(c.Request.Context(), auth.MustUserID(c.Request.Context()), id); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HouseholdHandler) ListMembers(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	include := c.Query("include") == "in_grace"
	listing, err := h.svc.ListMembers(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, include)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": listing})
}

func (h *HouseholdHandler) UpdateMemberRole(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	targetUserID, err := strconv.ParseInt(c.Param("userID"), 10, 64)
	if err != nil || targetUserID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	var req updateMemberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	mem, err := h.svc.UpdateMemberRole(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, targetUserID, req.Role)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mem})
}

func (h *HouseholdHandler) RemoveMember(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	targetUserID, err := strconv.ParseInt(c.Param("userID"), 10, 64)
	if err != nil || targetUserID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	if err := h.svc.RemoveMember(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, targetUserID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HouseholdHandler) TransferOwner(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req transferOwnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	if req.UserID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	if err := h.svc.TransferOwner(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, req.UserID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HouseholdHandler) CreateSharedBudget(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req createSharedBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	b, err := h.svc.CreateSharedBudget(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, household.SharedBudgetInput{
		CategoryID: req.CategoryID,
		Period:     req.Period,
		Amount:     req.Amount,
		Rollover:   req.Rollover,
		IsActive:   req.IsActive,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": b})
}

func (h *HouseholdHandler) ListSharedBudgets(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	out, err := h.svc.ListSharedBudgets(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

func (h *HouseholdHandler) UpdateSharedBudget(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	budgetID, err := strconv.ParseInt(c.Param("budgetID"), 10, 64)
	if err != nil || budgetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "budgetID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	var req updateSharedBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	b, err := h.svc.UpdateSharedBudget(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, budgetID, household.UpdateSharedBudgetInput{
		CategoryID: req.CategoryID,
		Period:     req.Period,
		Amount:     req.Amount,
		Rollover:   req.Rollover,
		IsActive:   req.IsActive,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *HouseholdHandler) DeleteSharedBudget(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	budgetID, err := strconv.ParseInt(c.Param("budgetID"), 10, 64)
	if err != nil || budgetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "budgetID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	if err := h.svc.SoftDeleteSharedBudget(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, budgetID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HouseholdHandler) CreateSharedGoal(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req createSharedGoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	in := household.SharedGoalInput{
		Name:         req.Name,
		TargetAmount: req.TargetAmount,
	}
	if req.TargetDate != nil && *req.TargetDate != "" {
		d, err := time.Parse("2006-01-02", *req.TargetDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "target_date must be YYYY-MM-DD", "code": "INVALID_REQUEST"})
			return
		}
		in.TargetDate = &d
	}
	g, err := h.svc.CreateSharedGoal(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, in)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": g})
}

func (h *HouseholdHandler) ListSharedGoals(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	out, err := h.svc.ListSharedGoals(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": int64(len(out))})
}

func (h *HouseholdHandler) UpdateSharedGoal(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	goalID, err := strconv.ParseInt(c.Param("goalID"), 10, 64)
	if err != nil || goalID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "goalID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	var req updateSharedGoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	in := household.UpdateSharedGoalInput{
		Name:            req.Name,
		TargetAmount:    req.TargetAmount,
		ClearTargetDate: req.ClearTargetDate,
	}
	if req.TargetDate != nil && *req.TargetDate != "" {
		d, err := time.Parse("2006-01-02", *req.TargetDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "target_date must be YYYY-MM-DD", "code": "INVALID_REQUEST"})
			return
		}
		in.TargetDate = &d
	}
	g, err := h.svc.UpdateSharedGoal(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, goalID, in)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": g})
}

func (h *HouseholdHandler) DeleteSharedGoal(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	goalID, err := strconv.ParseInt(c.Param("goalID"), 10, 64)
	if err != nil || goalID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "goalID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	if err := h.svc.SoftDeleteSharedGoal(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, goalID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *HouseholdHandler) ContributeToSharedGoal(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	goalID, err := strconv.ParseInt(c.Param("goalID"), 10, 64)
	if err != nil || goalID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "goalID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	var req sharedGoalContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	g, err := h.svc.ContributeToSharedGoal(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, goalID, req.Amount)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": g})
}

func (h *HouseholdHandler) ListShares(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	shares, err := h.svc.ListShares(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": shares, "total": int64(len(shares))})
}

func (h *HouseholdHandler) SetShare(c *gin.Context) {
	accountID, ok := parseID(c)
	if !ok {
		return
	}
	householdID, err := strconv.ParseInt(c.Param("householdID"), 10, 64)
	if err != nil || householdID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "householdID must be a positive integer", "code": "INVALID_REQUEST"})
		return
	}
	var req setShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	share, err := h.svc.SetShare(c.Request.Context(), auth.MustUserID(c.Request.Context()), accountID, householdID, req.Visibility)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if share == nil {
		// Visibility==private → row cleared (idempotent).
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": share})
}

// writeError maps household domain errors to HTTP status codes.
func (h *HouseholdHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, household.ErrHouseholdNotFound),
		errors.Is(err, household.ErrInviteNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "NOT_FOUND"})
	case errors.Is(err, household.ErrNotMember),
		errors.Is(err, household.ErrAccountNotOwned):
		// Treat as 404 so non-members can't fingerprint existence.
		c.JSON(http.StatusNotFound, gin.H{"error": "not found", "code": "NOT_FOUND"})
	case errors.Is(err, household.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "FORBIDDEN"})
	case errors.Is(err, household.ErrLastOwner):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "LAST_OWNER"})
	case errors.Is(err, household.ErrCannotModifySelf):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "CANNOT_MODIFY_SELF"})
	case errors.Is(err, household.ErrMemberNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "MEMBER_NOT_FOUND"})
	case errors.Is(err, household.ErrAlreadyMember):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "ALREADY_MEMBER"})
	case errors.Is(err, household.ErrInviteAlreadyAccepted):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "INVITE_ACCEPTED"})
	case errors.Is(err, household.ErrInviteExpired):
		c.JSON(http.StatusGone, gin.H{"error": err.Error(), "code": "INVITE_EXPIRED"})
	case errors.Is(err, household.ErrBudgetNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "BUDGET_NOT_FOUND"})
	case errors.Is(err, household.ErrSharedGoalNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "GOAL_NOT_FOUND"})
	case errors.Is(err, household.ErrUnknownCategory):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "UNKNOWN_CATEGORY"})
	case errors.Is(err, household.ErrInvalidBudgetPeriod),
		errors.Is(err, household.ErrInvalidBudgetAmount):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_BUDGET"})
	case errors.Is(err, household.ErrSharedGoalEmptyName),
		errors.Is(err, household.ErrSharedGoalInvalidTarget),
		errors.Is(err, household.ErrSharedGoalZeroContribution):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_GOAL"})
	case errors.Is(err, household.ErrEmptyName),
		errors.Is(err, household.ErrInvalidRole),
		errors.Is(err, household.ErrCannotPromoteToOwner),
		errors.Is(err, household.ErrInvalidGrace),
		errors.Is(err, household.ErrInvalidVisibility):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
	case errors.Is(err, household.ErrInstanceNotReady):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": "NOT_BOOTSTRAPPED"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
