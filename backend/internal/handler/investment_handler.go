package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type InvestmentHandler struct {
	svc *service.InvestmentService
}

func NewInvestmentHandler(s *service.InvestmentService) *InvestmentHandler {
	return &InvestmentHandler{svc: s}
}

func (h *InvestmentHandler) Register(g *gin.RouterGroup) {
	g.POST("/investments", h.Create)
	g.POST("/investments/import-csv", h.ImportCSV)
	g.GET("/investments", h.List)
	// /portfolio sits above /:id so the literal segment wins the route match.
	g.GET("/investments/portfolio", h.Portfolio)
	g.GET("/investments/:id", h.Get)
}

type createInvestmentRequest struct {
	AccountID    int64            `json:"account_id"`
	Ticker       string           `json:"ticker"`
	Name         *string          `json:"name"`
	AssetClass   *string          `json:"asset_class"`
	Quantity     decimal.Decimal  `json:"quantity"`
	CostBasis    *decimal.Decimal `json:"cost_basis"`
	MarketValue  *decimal.Decimal `json:"market_value"`
	SnapshotDate *string          `json:"snapshot_date"` // YYYY-MM-DD
	Source       string           `json:"source"`
}

func (h *InvestmentHandler) Create(c *gin.Context) {
	var req createInvestmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	in := service.CreateInvestmentInput{
		AccountID:   req.AccountID,
		Ticker:      req.Ticker,
		Name:        req.Name,
		AssetClass:  req.AssetClass,
		Quantity:    req.Quantity,
		CostBasis:   req.CostBasis,
		MarketValue: req.MarketValue,
		Source:      req.Source,
	}
	if req.SnapshotDate != nil && *req.SnapshotDate != "" {
		d, err := time.Parse("2006-01-02", *req.SnapshotDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "snapshot_date must be YYYY-MM-DD", "code": "INVALID_REQUEST"})
			return
		}
		in.SnapshotDate = d
	}
	inv, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), in)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": inv})
}

func (h *InvestmentHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	inv, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": inv})
}

// List dispatches on query params:
//   - No params → latest snapshot per holding for the user.
//   - ?account_id=N&ticker=AAPL → snapshot history for one holding.
func (h *InvestmentHandler) List(c *gin.Context) {
	userID := auth.MustUserID(c.Request.Context())
	accountQ := c.Query("account_id")
	tickerQ := c.Query("ticker")
	if accountQ != "" && tickerQ != "" {
		acctID, err := strconv.ParseInt(accountQ, 10, 64)
		if err != nil || acctID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "account_id must be a positive integer", "code": "INVALID_REQUEST"})
			return
		}
		rows, err := h.svc.ListSnapshots(c.Request.Context(), userID, acctID, tickerQ)
		if err != nil {
			h.writeServiceError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows, "total": int64(len(rows))})
		return
	}
	if accountQ != "" || tickerQ != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id and ticker must be provided together", "code": "INVALID_REQUEST"})
		return
	}
	rows, err := h.svc.ListLatest(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": int64(len(rows))})
}

// ImportCSV accepts a multipart "file" upload + optional ?account_id=N
// and returns counts + per-row errors. When account_id is missing we
// fall back to the user's lone investment-typed account; 0 or >1 such
// accounts → 400 MISSING_ACCOUNT_ID.
func (h *InvestmentHandler) ImportCSV(c *gin.Context) {
	userID := auth.MustUserID(c.Request.Context())

	// Cap the upload at 5 MiB — brokerage holdings CSVs are KB-scale.
	// Anything larger is almost certainly the wrong file.
	const maxUpload = 5 << 20
	if err := c.Request.ParseMultipartForm(maxUpload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_UPLOAD"})
		return
	}
	fileHdr, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing form field 'file'", "code": "INVALID_UPLOAD"})
		return
	}
	if fileHdr.Size > maxUpload {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large", "code": "FILE_TOO_LARGE"})
		return
	}
	f, err := fileHdr.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_UPLOAD"})
		return
	}
	defer f.Close()

	var accountID int64
	if q := c.Query("account_id"); q != "" {
		id, err := strconv.ParseInt(q, 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "account_id must be a positive integer", "code": "INVALID_REQUEST"})
			return
		}
		accountID = id
	} else {
		id, err := h.svc.ResolveInvestmentAccount(c.Request.Context(), userID)
		if err != nil {
			h.writeServiceError(c, err)
			return
		}
		accountID = id
	}

	result, err := h.svc.ImportCSV(c.Request.Context(), userID, accountID, f)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *InvestmentHandler) Portfolio(c *gin.Context) {
	summary, err := h.svc.PortfolioSummary(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": summary})
}

func (h *InvestmentHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvestmentNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "INVESTMENT_NOT_FOUND"})
	case errors.Is(err, service.ErrAccountNotFound):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "ACCOUNT_MISMATCH"})
	case errors.Is(err, service.ErrInvalidTicker),
		errors.Is(err, service.ErrZeroQuantity),
		errors.Is(err, service.ErrNegativeCostBasis),
		errors.Is(err, service.ErrNegativeMarketValue):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_INVESTMENT"})
	case errors.Is(err, service.ErrMissingAccountID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "MISSING_ACCOUNT_ID"})
	case errors.Is(err, service.ErrUnknownCSVFormat):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "UNKNOWN_CSV_FORMAT"})
	case errors.Is(err, service.ErrInvalidInvestmentSrc):
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   err.Error(),
			"code":    "INVALID_SOURCE",
			"allowed": []string{service.InvestmentSourcePlaid, service.InvestmentSourceCSV, service.InvestmentSourceManual},
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
