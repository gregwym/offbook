package service

import (
	"context"
	"errors"
	"fmt"
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
