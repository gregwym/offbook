package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// Investment source enum. Mirrors the CHECK constraint on
// investments.source from migration 000001.
const (
	InvestmentSourcePlaid  = "plaid"
	InvestmentSourceCSV    = "csv"
	InvestmentSourceManual = "manual"
)

var validInvestmentSources = map[string]struct{}{
	InvestmentSourcePlaid:  {},
	InvestmentSourceCSV:    {},
	InvestmentSourceManual: {},
}

var (
	ErrInvestmentNotFound   = errors.New("investment snapshot not found")
	ErrInvalidTicker        = errors.New("ticker must not be empty")
	ErrZeroQuantity         = errors.New("quantity must not be zero")
	ErrInvalidInvestmentSrc = errors.New("source must be one of: plaid, csv, manual")
	ErrNegativeCostBasis    = errors.New("cost_basis must be >= 0")
	ErrNegativeMarketValue  = errors.New("market_value must be >= 0")
)

// CreateInvestmentInput is the validated payload for snapshot creation.
type CreateInvestmentInput struct {
	AccountID    int64
	Ticker       string
	Name         *string
	AssetClass   *string
	Quantity     decimal.Decimal
	CostBasis    *decimal.Decimal
	MarketValue  *decimal.Decimal
	SnapshotDate time.Time
	Source       string
}

// InvestmentService owns snapshot validation + the append-only CRUD.
type InvestmentService struct {
	repo     repository.InvestmentRepository
	acctRepo repository.AccountRepository
}

func NewInvestmentService(repo repository.InvestmentRepository, acctRepo repository.AccountRepository) *InvestmentService {
	return &InvestmentService{repo: repo, acctRepo: acctRepo}
}

func (s *InvestmentService) Create(ctx context.Context, userID int64, in CreateInvestmentInput) (*model.Investment, error) {
	ticker := strings.ToUpper(strings.TrimSpace(in.Ticker))
	if ticker == "" {
		return nil, ErrInvalidTicker
	}
	if in.Quantity.IsZero() {
		return nil, ErrZeroQuantity
	}
	src := strings.TrimSpace(in.Source)
	if src == "" {
		src = InvestmentSourceManual
	}
	if _, ok := validInvestmentSources[src]; !ok {
		return nil, ErrInvalidInvestmentSrc
	}
	if in.CostBasis != nil && in.CostBasis.IsNegative() {
		return nil, ErrNegativeCostBasis
	}
	if in.MarketValue != nil && in.MarketValue.IsNegative() {
		return nil, ErrNegativeMarketValue
	}
	// Account ownership check — block cross-user account linkage.
	if _, err := s.acctRepo.GetByID(ctx, userID, in.AccountID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("validate account: %w", err)
	}
	snapshotDate := in.SnapshotDate
	if snapshotDate.IsZero() {
		snapshotDate = time.Now().UTC().Truncate(24 * time.Hour)
	}
	inv := &model.Investment{
		UserID:       userID,
		AccountID:    in.AccountID,
		Ticker:       ticker,
		Name:         trimPtr(in.Name),
		AssetClass:   trimPtr(in.AssetClass),
		Quantity:     in.Quantity,
		CostBasis:    in.CostBasis,
		MarketValue:  in.MarketValue,
		SnapshotDate: snapshotDate,
		Source:       src,
	}
	if err := s.repo.Create(ctx, inv); err != nil {
		return nil, fmt.Errorf("create investment: %w", err)
	}
	return inv, nil
}

func (s *InvestmentService) Get(ctx context.Context, userID, id int64) (*model.Investment, error) {
	inv, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvestmentNotFound
		}
		return nil, err
	}
	return inv, nil
}

func (s *InvestmentService) ListLatest(ctx context.Context, userID int64) ([]model.Investment, error) {
	return s.repo.ListLatestPerHolding(ctx, userID)
}

func (s *InvestmentService) ListSnapshots(ctx context.Context, userID, accountID int64, ticker string) ([]model.Investment, error) {
	// Verify the account belongs to the user before exposing snapshots —
	// otherwise a leak via an enumeration loop is possible.
	if _, err := s.acctRepo.GetByID(ctx, userID, accountID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("validate account: %w", err)
	}
	return s.repo.ListSnapshotsByHolding(ctx, userID, accountID, ticker)
}

// AssetClassAllocation is one slice of the portfolio donut.
type AssetClassAllocation struct {
	AssetClass  string          `json:"asset_class"`
	MarketValue decimal.Decimal `json:"market_value"`
	// WeightPct is the holding's share of TotalMarketValue, 0–100, with two
	// decimal places. Always a percent, never a fraction.
	WeightPct decimal.Decimal `json:"weight_pct"`
}

// PortfolioSummary is the response for GET /investments/portfolio.
// Totals are computed off the latest snapshot per (account_id, ticker)
// with quantity > 0. CostBasis nulls are excluded from TotalCostBasis;
// TotalUnrealizedGainLoss is nil unless at least one holding has both
// market_value and cost_basis populated.
type PortfolioSummary struct {
	TotalMarketValue        decimal.Decimal        `json:"total_market_value"`
	TotalCostBasis          decimal.Decimal        `json:"total_cost_basis"`
	TotalUnrealizedGainLoss *decimal.Decimal       `json:"total_unrealized_gain_loss"`
	HoldingsCount           int                    `json:"holdings_count"`
	ByAssetClass            []AssetClassAllocation `json:"by_asset_class"`
}

// PortfolioSummary computes per-user totals + asset-class allocation from
// the latest snapshot per holding. Holdings with quantity == 0 are
// dropped (closed positions whose history is preserved but that don't
// belong in the live portfolio view).
func (s *InvestmentService) PortfolioSummary(ctx context.Context, userID int64) (*PortfolioSummary, error) {
	holdings, err := s.repo.ListLatestPerHolding(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list latest: %w", err)
	}

	out := &PortfolioSummary{
		TotalMarketValue: decimal.Zero,
		TotalCostBasis:   decimal.Zero,
		ByAssetClass:     []AssetClassAllocation{},
	}

	byClass := map[string]decimal.Decimal{}
	gainLossAccum := decimal.Zero
	gainLossPresent := false

	for _, h := range holdings {
		if h.Quantity.IsZero() {
			continue
		}
		out.HoldingsCount++

		mv := decimal.Zero
		if h.MarketValue != nil {
			mv = *h.MarketValue
			out.TotalMarketValue = out.TotalMarketValue.Add(mv)
		}
		if h.CostBasis != nil {
			out.TotalCostBasis = out.TotalCostBasis.Add(*h.CostBasis)
		}
		if h.MarketValue != nil && h.CostBasis != nil {
			gainLossAccum = gainLossAccum.Add(h.MarketValue.Sub(*h.CostBasis))
			gainLossPresent = true
		}

		class := "Unclassified"
		if h.AssetClass != nil && strings.TrimSpace(*h.AssetClass) != "" {
			class = strings.TrimSpace(*h.AssetClass)
		}
		byClass[class] = byClass[class].Add(mv)
	}

	if gainLossPresent {
		gl := gainLossAccum
		out.TotalUnrealizedGainLoss = &gl
	}

	// Stable, deterministic order: descending by market value, ties broken
	// by class name. Makes API + tests predictable.
	classes := make([]string, 0, len(byClass))
	for k := range byClass {
		classes = append(classes, k)
	}
	sort.Slice(classes, func(i, j int) bool {
		ci, cj := byClass[classes[i]], byClass[classes[j]]
		if !ci.Equal(cj) {
			return ci.GreaterThan(cj)
		}
		return classes[i] < classes[j]
	})

	hundred := decimal.NewFromInt(100)
	for _, c := range classes {
		mv := byClass[c]
		weight := decimal.Zero
		if !out.TotalMarketValue.IsZero() {
			weight = mv.Div(out.TotalMarketValue).Mul(hundred).Round(2)
		}
		out.ByAssetClass = append(out.ByAssetClass, AssetClassAllocation{
			AssetClass:  c,
			MarketValue: mv,
			WeightPct:   weight,
		})
	}

	return out, nil
}
