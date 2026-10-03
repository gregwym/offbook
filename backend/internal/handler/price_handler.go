package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/prices"
)

// PriceHandler exposes the manual price refresh (ADR-0014 Phase 1). The
// refresh is user-initiated by design: clicking it is the egress consent —
// only the user's held symbols leave the box, never quantities or PII.
type PriceHandler struct {
	svc *prices.Service
}

func NewPriceHandler(s *prices.Service) *PriceHandler {
	return &PriceHandler{svc: s}
}

func (h *PriceHandler) Register(g *gin.RouterGroup) {
	g.POST("/prices/refresh", h.Refresh)
	g.POST("/assets/:id/prices", h.SetManual)
}

func (h *PriceHandler) Refresh(c *gin.Context) {
	result, err := h.svc.RefreshForUser(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		// Upstream provider failures surface as 502: the request was fine,
		// the price source wasn't. Valuations keep their stale flags.
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "code": "PRICE_PROVIDER_ERROR"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

type setManualPriceRequest struct {
	QuoteAssetID *int64           `json:"quote_asset_id"`
	Price        *decimal.Decimal `json:"price"`
	AsOf         string           `json:"as_of"`
}

// SetManual is the Tier-1 manual price-entry affordance (#373, ADR-0013 §5):
// the always-available pricing floor for an asset no provider covers and no
// trade has priced yet. Asset-scoped by URL; quote_asset_id defaults to the
// session user's primary currency when omitted.
func (h *PriceHandler) SetManual(c *gin.Context) {
	assetID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || assetID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid asset id", "code": "INVALID_REQUEST"})
		return
	}
	var req setManualPriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	if req.Price == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price is required", "code": "INVALID_REQUEST"})
		return
	}
	asOf, ok := parseFlexibleDate(c, req.AsOf, "as_of")
	if !ok {
		return
	}
	in := prices.SetManualPriceInput{
		AssetID:      assetID,
		QuoteAssetID: req.QuoteAssetID,
		Price:        *req.Price,
		AsOf:         asOf,
	}
	p, err := h.svc.SetManualPrice(c.Request.Context(), auth.MustUserID(c.Request.Context()), in)
	if err != nil {
		h.writeSetManualError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": p})
}

func (h *PriceHandler) writeSetManualError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, prices.ErrUnknownPriceAsset):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "UNKNOWN_ASSET"})
	case errors.Is(err, prices.ErrInvalidManualPrice),
		errors.Is(err, prices.ErrMissingAsOf),
		errors.Is(err, prices.ErrSameQuoteAsset):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_PRICE"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
