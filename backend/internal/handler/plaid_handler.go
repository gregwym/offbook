package handler

import (
	"errors"
	"net/http"
	"strconv"

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
	g.GET("/plaid/items", h.ListItems)
	g.DELETE("/plaid/items/:item_id", h.DisconnectItem)
	g.POST("/plaid/items/:item_id/sync-accounts", h.SyncAccounts)
	g.POST("/plaid/items/:item_id/sync-transactions", h.SyncTransactions)
	g.GET("/plaid/items/:item_id/errors", h.ListSyncErrors)
	g.POST("/plaid/errors/:error_id/retry", h.RetrySyncError)
	g.POST("/plaid/errors/:error_id/dismiss", h.DismissSyncError)
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
// {inserted, modified, removed, failed} count — never the transactions
// themselves. failed > 0 means rows landed in plaid_sync_errors; see
// GET /plaid/items/:id/errors.
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
			"failed":   result.Failed,
		},
	})
}

// ListItems returns the user's linked Plaid items for the Settings page,
// each annotated with its unresolved DLQ count so the UI can render the
// ⚠️ badge in one round trip (no per-item N+1). AccessToken is never
// included — model.PlaidItem has `json:"-"` on AccessTokenEnc.
func (h *PlaidHandler) ListItems(c *gin.Context) {
	userID := auth.MustUserID(c.Request.Context())
	items, err := h.svc.ListItemsWithSyncErrors(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": len(items)})
}

// ListSyncErrors returns the DLQ rows for one Plaid item. Query parameter
// ?status=unresolved (default) hides retried/dismissed rows; ?status=all
// returns the full history. The raw_payload is the full Plaid transaction
// JSON, echoed back so the modal can show it as-is.
func (h *PlaidHandler) ListSyncErrors(c *gin.Context) {
	plaidItemID := c.Param("item_id")
	if plaidItemID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_id is required", "code": "INVALID_REQUEST"})
		return
	}
	// Default to unresolved-only — the badge-driven flow wants actionable
	// rows. ?status=all opens the door for an audit view later.
	unresolvedOnly := true
	switch c.Query("status") {
	case "", "unresolved":
		unresolvedOnly = true
	case "all":
		unresolvedOnly = false
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be 'unresolved' or 'all'", "code": "INVALID_REQUEST"})
		return
	}
	userID := auth.MustUserID(c.Request.Context())
	rows, err := h.svc.ListSyncErrors(c.Request.Context(), userID, plaidItemID, unresolvedOnly)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": len(rows)})
}

// RetrySyncError replays the row's raw_payload through the same mapping
// path as a live sync. On success the row is marked resolved=retried_ok
// (204). On a known replay failure (payload still bad) the row stays
// unresolved and the handler returns 422 so the UI can keep the row in
// the list.
func (h *PlaidHandler) RetrySyncError(c *gin.Context) {
	id, ok := parseErrorID(c)
	if !ok {
		return
	}
	userID := auth.MustUserID(c.Request.Context())
	if err := h.svc.RetrySyncError(c.Request.Context(), userID, id); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DismissSyncError marks the row resolved=dismissed without replaying it.
// 204 on success; 404 if the row is missing or already resolved.
func (h *PlaidHandler) DismissSyncError(c *gin.Context) {
	id, ok := parseErrorID(c)
	if !ok {
		return
	}
	userID := auth.MustUserID(c.Request.Context())
	if err := h.svc.DismissSyncError(c.Request.Context(), userID, id); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// parseErrorID extracts and validates the :error_id path param.
// Writes the 400 response itself; returns ok=false when invalid.
func parseErrorID(c *gin.Context) (int64, bool) {
	raw := c.Param("error_id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "error_id must be a positive integer", "code": "INVALID_REQUEST"})
		return 0, false
	}
	return id, true
}

// DisconnectItem soft-deletes the link. Accounts previously synced remain
// visible; only the upstream connection is severed. 204 on success.
func (h *PlaidHandler) DisconnectItem(c *gin.Context) {
	plaidItemID := c.Param("item_id")
	if plaidItemID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_id is required", "code": "INVALID_REQUEST"})
		return
	}
	userID := auth.MustUserID(c.Request.Context())
	if err := h.svc.DisconnectItem(c.Request.Context(), userID, plaidItemID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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
	case errors.Is(err, plaidsvc.ErrSyncErrorNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
			"code":  "PLAID_SYNC_ERROR_NOT_FOUND",
		})
	case errors.Is(err, plaidsvc.ErrSyncErrorReplay):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": err.Error(),
			"code":  "PLAID_SYNC_ERROR_REPLAY",
		})
	default:
		c.JSON(http.StatusBadGateway, gin.H{
			"error": err.Error(),
			"code":  "PLAID_UPSTREAM",
		})
	}
}
