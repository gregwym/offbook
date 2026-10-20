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
	g.POST("/categorization-rules/preview", h.PreviewDraft)
}

type applyRulesRequest struct {
	Scope  string `json:"scope"`
	DryRun bool   `json:"dry_run"`
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
	run := h.svc.Apply
	if req.DryRun {
		run = h.svc.Preview
	}
	result, err := run(c.Request.Context(), auth.MustUserID(c.Request.Context()), req.Scope)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

type previewDraftRequest struct {
	RuleID     *int64 `json:"rule_id"`
	Pattern    string `json:"pattern"`
	MatchType  string `json:"match_type"`
	CategoryID int64  `json:"category_id"`
	AssetID    *int64 `json:"asset_id"`
	// RenameMerchant: optional merchant rename on match (#534).
	RenameMerchant *string `json:"rename_merchant"`
	Priority       int     `json:"priority"`
	IsActive       *bool   `json:"is_active"`
}

// PreviewDraft reports the impact of an unsaved rule (create or edit)
// without writing anything.
func (h *CategorizationRuleHandler) PreviewDraft(c *gin.Context) {
	var req previewDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	active := req.IsActive == nil || *req.IsActive
	result, err := h.svc.PreviewDraft(c.Request.Context(), auth.MustUserID(c.Request.Context()), service.DraftRuleInput{
		RuleID:         req.RuleID,
		Pattern:        req.Pattern,
		MatchType:      req.MatchType,
		CategoryID:     req.CategoryID,
		AssetID:        req.AssetID,
		RenameMerchant: req.RenameMerchant,
		Priority:       req.Priority,
		IsActive:       active,
	})
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
	// RenameMerchant: optional merchant rename on match (#534).
	RenameMerchant *string `json:"rename_merchant"`
	Priority       int     `json:"priority"`
	IsActive       *bool   `json:"is_active"`
}

func (h *CategorizationRuleHandler) Create(c *gin.Context) {
	var req createRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	r, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), service.CreateRuleInput{
		Pattern:        req.Pattern,
		MatchType:      req.MatchType,
		CategoryID:     req.CategoryID,
		AssetID:        req.AssetID,
		RenameMerchant: req.RenameMerchant,
		Priority:       req.Priority,
		IsActive:       req.IsActive,
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
	// RenameMerchant: a blank string clears the rename (#534).
	RenameMerchant *string `json:"rename_merchant"`
	Priority       *int    `json:"priority"`
	IsActive       *bool   `json:"is_active"`
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
		Pattern:        req.Pattern,
		MatchType:      req.MatchType,
		CategoryID:     req.CategoryID,
		AssetID:        req.AssetID,
		ClearAssetID:   req.ClearAssetID,
		RenameMerchant: req.RenameMerchant,
		Priority:       req.Priority,
		IsActive:       req.IsActive,
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
		errors.Is(err, service.ErrInvalidPriority),
		errors.Is(err, service.ErrRenameTooLong):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_RULE"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
