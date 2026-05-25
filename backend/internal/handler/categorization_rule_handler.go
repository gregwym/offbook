package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type CategorizationRuleHandler struct {
	svc *service.CategorizationRuleService
}

func NewCategorizationRuleHandler(s *service.CategorizationRuleService) *CategorizationRuleHandler {
	return &CategorizationRuleHandler{svc: s}
}

func (h *CategorizationRuleHandler) Register(g *gin.RouterGroup) {
	g.POST("/categorization-rules", h.Create)
	g.GET("/categorization-rules", h.List)
	g.GET("/categorization-rules/:id", h.Get)
	g.PATCH("/categorization-rules/:id", h.Update)
	g.DELETE("/categorization-rules/:id", h.Delete)
	g.POST("/categorization-rules/apply", h.Apply)
}

type applyRulesRequest struct {
	Scope string `json:"scope"`
}

// Apply runs the bulk re-categorize endpoint. Empty body defaults to
// scope="all"; explicit scope ∈ {all, uncategorized, plaid_default}.
func (h *CategorizationRuleHandler) Apply(c *gin.Context) {
	var req applyRulesRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
			return
		}
	}
	result, err := h.svc.Apply(c.Request.Context(), auth.MustUserID(c.Request.Context()), req.Scope)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

type createRuleRequest struct {
	Pattern    string `json:"pattern"`
	MatchType  string `json:"match_type"`
	CategoryID int64  `json:"category_id"`
	AssetID    *int64 `json:"asset_id"`
	Priority   int    `json:"priority"`
	IsActive   *bool  `json:"is_active"`
}

func (h *CategorizationRuleHandler) Create(c *gin.Context) {
	var req createRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	r, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), service.CreateRuleInput{
		Pattern:    req.Pattern,
		MatchType:  req.MatchType,
		CategoryID: req.CategoryID,
		AssetID:    req.AssetID,
		Priority:   req.Priority,
		IsActive:   req.IsActive,
	})
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": r})
}

func (h *CategorizationRuleHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	r, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *CategorizationRuleHandler) List(c *gin.Context) {
	rules, err := h.svc.List(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rules, "total": int64(len(rules))})
}

type updateRuleRequest struct {
	Pattern      *string `json:"pattern"`
	MatchType    *string `json:"match_type"`
	CategoryID   *int64  `json:"category_id"`
	AssetID      *int64  `json:"asset_id"`
	ClearAssetID bool    `json:"clear_asset_id"`
	Priority     *int    `json:"priority"`
	IsActive     *bool   `json:"is_active"`
}

func (h *CategorizationRuleHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req updateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	r, err := h.svc.Update(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, service.UpdateRuleInput{
		Pattern:      req.Pattern,
		MatchType:    req.MatchType,
		CategoryID:   req.CategoryID,
		AssetID:      req.AssetID,
		ClearAssetID: req.ClearAssetID,
		Priority:     req.Priority,
		IsActive:     req.IsActive,
	})
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *CategorizationRuleHandler) Delete(c *gin.Context) {
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

func (h *CategorizationRuleHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrRuleNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "RULE_NOT_FOUND"})
	case errors.Is(err, service.ErrInvalidRegex):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REGEX"})
	case errors.Is(err, service.ErrUnknownCategory):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "UNKNOWN_CATEGORY"})
	case errors.Is(err, service.ErrInvalidApplyScope):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_SCOPE"})
	case errors.Is(err, service.ErrEmptyPattern),
		errors.Is(err, service.ErrInvalidMatchType),
		errors.Is(err, service.ErrInvalidPriority):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_RULE"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
