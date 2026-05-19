package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/ingestion"
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
	ErrMissingAccountID     = errors.New("account_id is required: user has no unique investment account")
	ErrUnknownCSVFormat     = errors.New("unknown CSV format: expected Vanguard or Fidelity holdings export")
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
	// RecentChange tracks the most-recent snapshot delta — the wireframe
	// calls this "today's P&L". Without a live price feed we measure
	// "today" as "between the two most recent snapshot dates per
	// holding". Nil when no holding has two snapshots to compare.
	RecentChange *RecentChange `json:"recent_change,omitempty"`
}

// RecentChange is the per-holding rollup of the latest-vs-prior snapshot
// pair. Holdings with only one snapshot contribute nothing.
type RecentChange struct {
	// Delta is the sum of (latest.market_value - prior.market_value)
	// across holdings that have both snapshots.
	Delta decimal.Decimal `json:"delta"`
	// HoldingsCompared is how many holdings the delta is computed across.
	HoldingsCompared int `json:"holdings_compared"`
	// Up / Down / Flat counts give the wireframe's "X of Y up" line.
	Up   int `json:"up"`
	Down int `json:"down"`
	Flat int `json:"flat"`
	// LatestDate / PriorDate are the canonical labels — the max() of
	// each side across all paired holdings. Different holdings may have
	// different snapshot dates; this picks the most-recent one on each
	// side so the UI can label the period honestly.
	LatestDate time.Time `json:"latest_date"`
	PriorDate  time.Time `json:"prior_date"`
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

	// Recent-change is best-effort: a query failure here shouldn't fail
	// the whole summary endpoint. The dashboard tile renders "—" when
	// RecentChange is nil.
	if rc, err := s.recentChange(ctx, userID); err == nil {
		out.RecentChange = rc
	}

	return out, nil
}

// recentChange pairs each holding's two most-recent snapshots (when both
// exist) and sums the deltas. Holdings with only one snapshot contribute
// nothing — same as holdings with quantity == 0 (excluded). Returns nil
// when no holding has a pair to compare.
func (s *InvestmentService) recentChange(ctx context.Context, userID int64) (*RecentChange, error) {
	pairs, err := s.repo.ListLatestPairPerHolding(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list pairs: %w", err)
	}
	rc := &RecentChange{Delta: decimal.Zero}
	// Pairs come ordered (account_id, ticker, date DESC) — every odd row
	// is the latest, every even row (when present) is the prior.
	for i := 0; i < len(pairs); {
		latest := &pairs[i]
		// Find the matching prior (same account_id + ticker, next row).
		var prior *model.Investment
		if i+1 < len(pairs) &&
			pairs[i+1].AccountID == latest.AccountID &&
			strings.EqualFold(pairs[i+1].Ticker, latest.Ticker) {
			prior = &pairs[i+1]
			i += 2
		} else {
			i++
		}
		if prior == nil {
			continue // single-snapshot holding
		}
		if latest.Quantity.IsZero() {
			continue // closed position
		}
		if latest.MarketValue == nil || prior.MarketValue == nil {
			continue // no MV on one side → can't measure
		}
		d := latest.MarketValue.Sub(*prior.MarketValue)
		rc.Delta = rc.Delta.Add(d)
		rc.HoldingsCompared++
		switch {
		case d.IsPositive():
			rc.Up++
		case d.IsNegative():
			rc.Down++
		default:
			rc.Flat++
		}
		if latest.SnapshotDate.After(rc.LatestDate) {
			rc.LatestDate = latest.SnapshotDate
		}
		if prior.SnapshotDate.After(rc.PriorDate) {
			rc.PriorDate = prior.SnapshotDate
		}
	}
	if rc.HoldingsCompared == 0 {
		return nil, nil
	}
	return rc, nil
}

// ImportResult mirrors the JSON contract for the CSV import endpoint.
type ImportResult struct {
	Imported int                  `json:"imported"`
	Skipped  int                  `json:"skipped"`
	Errors   []ingestion.RowError `json:"errors"`
	Format   string               `json:"format"`
}

// ResolveInvestmentAccount picks an account for an import when the caller
// didn't supply one. Returns the lone investment-typed account or
// ErrMissingAccountID when there are 0 or >1 matches. Used by both the
// handler and CSV import to honor the "single investment account =
// implicit destination" UX from #115.
func (s *InvestmentService) ResolveInvestmentAccount(ctx context.Context, userID int64) (int64, error) {
	accounts, _, err := s.acctRepo.List(ctx, userID, repository.AccountFilter{AccountType: "investment", Limit: 2})
	if err != nil {
		return 0, fmt.Errorf("list investment accounts: %w", err)
	}
	if len(accounts) != 1 {
		return 0, ErrMissingAccountID
	}
	return accounts[0].ID, nil
}

// ImportCSV parses a Vanguard or Fidelity holdings export and creates one
// snapshot per row (source='csv'). Each row is validated and persisted
// independently; per-row errors are accumulated into the result instead
// of aborting the whole import, so a single bad row doesn't lose the
// good ones. Each row is its own single-row insert (no cross-row
// invariant), so the GORM connection's SkipDefaultTransaction is fine
// and we don't wrap in db.Transaction.
func (s *InvestmentService) ImportCSV(ctx context.Context, userID, accountID int64, r io.Reader) (*ImportResult, error) {
	// Confirm the destination account belongs to the user before parsing.
	if _, err := s.acctRepo.GetByID(ctx, userID, accountID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("validate account: %w", err)
	}

	parsed, err := ingestion.ParseHoldingsCSV(r)
	if err != nil {
		if errors.Is(err, ingestion.ErrUnknownCSVFormat) {
			return nil, ErrUnknownCSVFormat
		}
		return nil, fmt.Errorf("parse csv: %w", err)
	}

	result := &ImportResult{
		Format: parsed.Format,
		Errors: append([]ingestion.RowError{}, parsed.Errors...),
	}

	for i, h := range parsed.Holdings {
		in := CreateInvestmentInput{
			AccountID:    accountID,
			Ticker:       h.Ticker,
			Quantity:     h.Quantity,
			CostBasis:    h.CostBasis,
			MarketValue:  h.MarketValue,
			SnapshotDate: parsed.SnapshotDate,
			Source:       InvestmentSourceCSV,
		}
		if h.Name != "" {
			n := h.Name
			in.Name = &n
		}
		if h.AssetClass != "" {
			c := h.AssetClass
			in.AssetClass = &c
		}
		if _, err := s.Create(ctx, userID, in); err != nil {
			// Surface the broker symbol + the validation error so the
			// frontend can render a list of offending rows.
			result.Errors = append(result.Errors, ingestion.RowError{
				Line:    i + 1, // 1-based holding index within parsed set
				Message: fmt.Sprintf("%s: %s", h.Ticker, err.Error()),
			})
			result.Skipped++
			continue
		}
		result.Imported++
	}
	return result, nil
}
