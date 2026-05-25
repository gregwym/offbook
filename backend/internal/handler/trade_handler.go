package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// TradeHandler exposes the manual trade-entry surface introduced in
// #238. It lives under /accounts/:id/trades because trades belong to a
// specific account — the account scopes both authorization (the
// session user must own it) and the cash leg's asset.
type TradeHandler struct {
	svc *service.TradeService
}

func NewTradeHandler(s *service.TradeService) *TradeHandler {
	return &TradeHandler{svc: s}
}

func (h *TradeHandler) Register(g *gin.RouterGroup) {
	g.POST("/accounts/:id/trades", h.Create)
}

type createTradeRequest struct {
	Kind      string           `json:"kind"`
	AssetID   int64            `json:"asset_id"`
	Quantity  *decimal.Decimal `json:"quantity"`
	Price     *decimal.Decimal `json:"price"`
	TradeDate string           `json:"trade_date"`
	Notes     *string          `json:"notes"`
}

func (h *TradeHandler) Create(c *gin.Context) {
	idStr := c.Param("id")
	accountID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || accountID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id", "code": "INVALID_REQUEST"})
		return
	}
	var req createTradeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	if req.Quantity == nil || req.Price == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity and price are required", "code": "INVALID_REQUEST"})
		return
	}
	tradeDate, ok := parseFlexibleDate(c, req.TradeDate, "trade_date")
	if !ok {
		return
	}
	in := service.RecordTradeInput{
		Kind:      req.Kind,
		AssetID:   req.AssetID,
		Quantity:  *req.Quantity,
		Price:     *req.Price,
		TradeDate: tradeDate,
		Notes:     req.Notes,
	}
	rec, err := h.svc.Record(c.Request.Context(), auth.MustUserID(c.Request.Context()), accountID, in)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": rec})
}

func (h *TradeHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "ACCOUNT_NOT_FOUND"})
	case errors.Is(err, service.ErrUnknownAsset):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "UNKNOWN_ASSET"})
	case errors.Is(err, service.ErrUnsupportedAccount):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "UNSUPPORTED_ACCOUNT"})
	case errors.Is(err, service.ErrInvalidTradeKind),
		errors.Is(err, service.ErrInvalidQuantity),
		errors.Is(err, service.ErrInvalidPrice),
		errors.Is(err, service.ErrSecurityEqualsQuote),
		errors.Is(err, service.ErrMissingDate):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_TRADE"})
	case errors.Is(err, service.ErrSellExceedsHoldings):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INSUFFICIENT_HOLDING"})
	case errors.Is(err, service.ErrTradeFXUnavailable):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "code": "FX_UNAVAILABLE"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
