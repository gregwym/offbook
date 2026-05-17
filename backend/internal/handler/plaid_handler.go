package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type PlaidHandler struct {
	svc *plaidsvc.Service
}

func NewPlaidHandler(s *plaidsvc.Service) *PlaidHandler {
	return &PlaidHandler{svc: s}
}

// Register wires the Plaid Link flow under the secured /api/v1 group.
// The service may be unconfigured on instances without PLAID_CLIENT_ID; in
// that case the handlers 503 with a clear code so the frontend can hide
// the Connect button.
func (h *PlaidHandler) Register(g *gin.RouterGroup) {
	g.POST("/plaid/link/token", h.CreateLinkToken)
	g.POST("/plaid/link/exchange", h.ExchangePublicToken)
}

func (h *PlaidHandler) CreateLinkToken(c *gin.Context) {
	userID := auth.MustUserID(c.Request.Context())
	tok, err := h.svc.CreateLinkToken(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"link_token": tok.Token,
			"expiration": tok.Expiration,
		},
	})
}

type exchangePublicTokenRequest struct {
	PublicToken string `json:"public_token"`
}

func (h *PlaidHandler) ExchangePublicToken(c *gin.Context) {
	var req exchangePublicTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	userID := auth.MustUserID(c.Request.Context())
	item, err := h.svc.ExchangePublicToken(c.Request.Context(), userID, req.PublicToken)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// Deliberately narrow response shape: callers should not need (or want)
	// the access_token or any other internal fields. item_id is enough to
	// reference this connection in subsequent calls.
	c.JSON(http.StatusCreated, gin.H{
		"data": gin.H{
			"id":            item.ID,
			"item_id":       item.PlaidItemID,
			"institution":   item.InstitutionName,
			"status":        item.Status,
		},
	})
}

func (h *PlaidHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plaidsvc.ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": err.Error(),
			"code":  "PLAID_NOT_CONFIGURED",
		})
	case errors.Is(err, plaidsvc.ErrInvalidPublicToken):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
			"code":  "INVALID_REQUEST",
		})
	default:
		c.JSON(http.StatusBadGateway, gin.H{
			"error": err.Error(),
			"code":  "PLAID_UPSTREAM",
		})
	}
}
