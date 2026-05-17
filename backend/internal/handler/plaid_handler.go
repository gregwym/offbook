package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service/auth"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
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
	g.POST("/plaid/items/:item_id/sync-accounts", h.SyncAccounts)
	g.POST("/plaid/items/:item_id/sync-transactions", h.SyncTransactions)
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
			"id":          item.ID,
			"item_id":     item.PlaidItemID,
			"institution": item.InstitutionName,
			"status":      item.Status,
		},
	})
}

// SyncAccounts pulls /accounts/get (+ best-effort /identity/get) for the
// given item and upserts matching accounts rows. Response is intentionally
// narrow: {created, updated} counts, no account details, no PII.
func (h *PlaidHandler) SyncAccounts(c *gin.Context) {
	plaidItemID := c.Param("item_id")
	if plaidItemID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_id is required", "code": "INVALID_REQUEST"})
		return
	}
	userID := auth.MustUserID(c.Request.Context())
	result, err := h.svc.SyncAccounts(c.Request.Context(), userID, plaidItemID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"created": result.Created,
			"updated": result.Updated,
		},
	})
}

// SyncTransactions runs /transactions/sync for the given item, persisting
// added transactions and the resulting cursor. Response is a narrow
// {inserted, modified, removed} count — never the transactions themselves.
func (h *PlaidHandler) SyncTransactions(c *gin.Context) {
	plaidItemID := c.Param("item_id")
	if plaidItemID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_id is required", "code": "INVALID_REQUEST"})
		return
	}
	userID := auth.MustUserID(c.Request.Context())
	result, err := h.svc.SyncTransactions(c.Request.Context(), userID, plaidItemID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"inserted": result.Inserted,
			"modified": result.Modified,
			"removed":  result.Removed,
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
	case errors.Is(err, plaidsvc.ErrItemNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
			"code":  "PLAID_ITEM_NOT_FOUND",
		})
	default:
		c.JSON(http.StatusBadGateway, gin.H{
			"error": err.Error(),
			"code":  "PLAID_UPSTREAM",
		})
	}
}
