package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// AssetHandler exposes the global assets reference table to authenticated
// users. Assets are not user-scoped — symbols like AAPL/USD/BTC are shared
// across the instance — but the routes still live under /api/v1 so they
// require a session. The trade form uses these endpoints to resolve an
// asset_id for the request payload.
type AssetHandler struct {
	repo repository.AssetRepository
}

func NewAssetHandler(repo repository.AssetRepository) *AssetHandler {
	return &AssetHandler{repo: repo}
}

func (h *AssetHandler) Register(g *gin.RouterGroup) {
	g.GET("/assets", h.List)
	g.POST("/assets/ensure", h.Ensure)
}

// validAssetKinds mirrors the enum in migration 000018 and the
// model.AssetKind* constants.
var validAssetKinds = map[string]struct{}{
	model.AssetKindFiat:      {},
	model.AssetKindEquity:    {},
	model.AssetKindFund:      {},
	model.AssetKindCrypto:    {},
	model.AssetKindBond:      {},
	model.AssetKindCommodity: {},
	model.AssetKindOther:     {},
}

func (h *AssetHandler) List(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	var (
		assets []model.Asset
		err    error
	)
	if kind == "" {
		assets, err = h.repo.List(c.Request.Context())
	} else {
		if _, ok := validAssetKinds[kind]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid kind", "code": "INVALID_REQUEST"})
			return
		}
		assets, err = h.repo.ListByKind(c.Request.Context(), kind)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": assets, "total": int64(len(assets))})
}

type ensureAssetRequest struct {
	Symbol      string `json:"symbol"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
}

// Ensure is the find-or-create path the trade form uses when the user
// enters a previously-unseen ticker. It is idempotent: repeated calls with
// the same (symbol, kind) return the existing row.
func (h *AssetHandler) Ensure(c *gin.Context) {
	var req ensureAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is required", "code": "INVALID_REQUEST"})
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if _, ok := validAssetKinds[kind]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind is required and must be a known asset kind", "code": "INVALID_REQUEST"})
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = symbol
	}
	a, err := h.repo.EnsureBySymbolKind(c.Request.Context(), symbol, kind, displayName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": a})
}
