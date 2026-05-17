package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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
//   POST   /households                        create
//   GET    /households/:id                    detail (member-only)
//   PATCH  /households/:id                    update name / grace (owner)
//   DELETE /households/:id                    dissolve (owner)
//   POST   /households/:id/invites            mint invite token (owner)
//   POST   /invites/:token/accept             consume token
//   DELETE /households/:id/members/me         self-leave
//
//   GET    /accounts/:id/shares                       list visibility per household
//   PUT    /accounts/:id/shares/:householdID          set/clear visibility
func (h *HouseholdHandler) Register(g *gin.RouterGroup) {
	g.POST("/households", h.Create)
	g.GET("/households/:id", h.Get)
	g.PATCH("/households/:id", h.Update)
	g.DELETE("/households/:id", h.Dissolve)
	g.POST("/households/:id/invites", h.CreateInvite)
	g.POST("/invites/:token/accept", h.AcceptInvite)
	g.DELETE("/households/:id/members/me", h.LeaveSelf)

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
	case errors.Is(err, household.ErrAlreadyMember):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "ALREADY_MEMBER"})
	case errors.Is(err, household.ErrInviteAlreadyAccepted):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "INVITE_ACCEPTED"})
	case errors.Is(err, household.ErrInviteExpired):
		c.JSON(http.StatusGone, gin.H{"error": err.Error(), "code": "INVITE_EXPIRED"})
	case errors.Is(err, household.ErrEmptyName),
		errors.Is(err, household.ErrInvalidRole),
		errors.Is(err, household.ErrInvalidGrace),
		errors.Is(err, household.ErrInvalidVisibility):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
	case errors.Is(err, household.ErrInstanceNotReady):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": "NOT_BOOTSTRAPPED"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
