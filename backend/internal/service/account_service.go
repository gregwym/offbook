package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// Domain errors. Handlers map these to HTTP status codes.
var (
	ErrAccountNotFound  = errors.New("account not found")
	ErrInvalidAccount   = errors.New("invalid account")
	ErrInvalidType      = errors.New("invalid account_type")
	ErrInvalidCurrency  = errors.New("invalid currency")
	ErrInvalidLastFour  = errors.New("last_four must be exactly 4 digits")
	ErrEmptyName        = errors.New("name must not be empty")
	ErrEmptyInstitution = errors.New("institution_slug must not be empty")
)

// validAccountTypes mirrors the CHECK constraint in migration 000001.
// Per ADR-0013, account_type is a display hint (which UI to render), not a
// data-shape switch — schema treats every account as a bag of positions.
var validAccountTypes = map[string]struct{}{
	"checking":    {},
	"savings":     {},
	"credit_card": {},
	"loan":        {},
	"investment":  {},
	"crypto":      {},
	"cash":        {},
	"other":       {},
}

// CreateAccountInput is the validated, decoded request payload for account
// creation. OpeningBalance, when non-zero, seeds a position in the account's
// primary quote asset.
type CreateAccountInput struct {
	Name            string
	InstitutionSlug string
	AccountType     string
	Currency        string
	OpeningBalance  decimal.Decimal
	LastFour        *string
	IsActive        *bool
}

// UpdateAccountInput is a sparse patch. Pointer fields distinguish "not provided"
// from "set to zero value". Balance is intentionally absent — per ADR-0013
// account balance is derived from positions × prices; to change it, write a
// transaction or update the position directly.
type UpdateAccountInput struct {
	Name            *string
	InstitutionSlug *string
	AccountType     *string
	Currency        *string
	LastFour        *string
	IsActive        *bool
}

// AccountService owns business rules for accounts. It deliberately does NOT
// receive pii_repo or pii_service — PII is set via the separate pii endpoints.
// All operations are scoped to a user_id derived from the session.
type AccountService struct {
	db            *gorm.DB
	repo          repository.AccountRepository
	assetRepo     repository.AssetRepository
	positionRepo  repository.PositionRepository
	plaidItemRepo repository.PlaidItemRepository
	valuationSvc  *valuation.Service
}

func NewAccountService(
	db *gorm.DB,
	repo repository.AccountRepository,
	assetRepo repository.AssetRepository,
	positionRepo repository.PositionRepository,
) *AccountService {
	return &AccountService{db: db, repo: repo, assetRepo: assetRepo, positionRepo: positionRepo}
}

// WithPlaidItemRepo lets the router inject the optional dependency without
// breaking every caller of NewAccountService. Returns the service so the
// construction stays a one-liner.
func (s *AccountService) WithPlaidItemRepo(p repository.PlaidItemRepository) *AccountService {
	s.plaidItemRepo = p
	return s
}

// WithValuation injects the valuation service that derives each account's
// balance from positions × prices (ADR-0013). Same optional-dependency
// pattern as WithPlaidItemRepo; without it, responses carry a zero balance
// marked incomplete so it can't be mistaken for a real total.
func (s *AccountService) WithValuation(v *valuation.Service) *AccountService {
	s.valuationSvc = v
	return s
}

func (s *AccountService) Create(ctx context.Context, userID int64, in CreateAccountInput) (*model.Account, error) {
	if in.Currency == "" {
		in.Currency = "USD"
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))

	// Resolve the fiat asset for the account's currency; create on first
	// encounter so users with exotic currencies aren't blocked.
	asset, err := s.assetRepo.EnsureBySymbolKind(ctx, currency, model.AssetKindFiat, currency)
	if err != nil {
		return nil, fmt.Errorf("resolve currency asset: %w", err)
	}

	a := &model.Account{
		UserID:              userID,
		Name:                strings.TrimSpace(in.Name),
		InstitutionSlug:     strings.TrimSpace(in.InstitutionSlug),
		AccountType:         strings.TrimSpace(in.AccountType),
		Currency:            currency,
		PrimaryQuoteAssetID: asset.ID,
		LastFour:            in.LastFour,
		IsActive:            true,
	}
	if in.IsActive != nil {
		a.IsActive = *in.IsActive
	}
	if err := validate(a); err != nil {
		return nil, err
	}

	// Create the account row and (when there's an opening balance) seed an
	// initial cash-position in the same DB transaction so a mid-flight
	// failure rolls back both writes.
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		acctRepo := repository.NewAccountRepository(tx)
		if err := acctRepo.Create(ctx, a); err != nil {
			return fmt.Errorf("create account: %w", err)
		}
		if !in.OpeningBalance.IsZero() {
			// Record the opening balance as a real ledger fact (ADR-0017):
			// an opening_balance transaction is the anchor the position folds
			// from. kind=opening_balance keeps it out of spending/cash-flow/
			// budget analytics while still counting toward net worth.
			txRepo := repository.NewTransactionRepository(tx)
			desc := "Opening balance"
			openingTx := &model.Transaction{
				UserID:          userID,
				AccountID:       a.ID,
				AssetID:         asset.ID,
				Kind:            model.KindOpeningBalance,
				Amount:          in.OpeningBalance,
				Description:     &desc,
				TransactionDate: time.Now().UTC(),
				Source:          "manual",
			}
			if err := txRepo.Create(ctx, openingTx); err != nil {
				return fmt.Errorf("seed opening balance transaction: %w", err)
			}
			posRepo := repository.NewPositionRepository(tx)
			pos := &model.Position{
				UserID:    userID,
				AccountID: a.ID,
				AssetID:   asset.ID,
				Quantity:  in.OpeningBalance,
			}
			if err := posRepo.Upsert(ctx, pos); err != nil {
				return fmt.Errorf("seed opening position: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *AccountService) Get(ctx context.Context, userID, id int64) (*model.Account, error) {
	a, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	if err := s.hydrateCurrency(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *AccountService) List(ctx context.Context, userID int64, f repository.AccountFilter) ([]model.Account, int64, error) {
	accounts, total, err := s.repo.List(ctx, userID, f)
	if err != nil {
		return nil, 0, err
	}
	ptrs := make([]*model.Account, len(accounts))
	for i := range accounts {
		ptrs[i] = &accounts[i]
	}
	if err := s.hydrateCurrency(ctx, ptrs...); err != nil {
		return nil, 0, err
	}
	return accounts, total, nil
}

// hydrateCurrency fills each account's derived Currency from its primary
// quote asset's symbol (#284 — currency is no longer a stored column). One
// asset-table read serves any number of accounts, so List stays O(1) queries.
func (s *AccountService) hydrateCurrency(ctx context.Context, accts ...*model.Account) error {
	if len(accts) == 0 {
		return nil
	}
	assets, err := s.assetRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("load assets for currency: %w", err)
	}
	symbolByID := make(map[int64]string, len(assets))
	for _, a := range assets {
		symbolByID[a.ID] = a.Symbol
	}
	for _, a := range accts {
		a.Currency = symbolByID[a.PrimaryQuoteAssetID]
	}
	return nil
}

func (s *AccountService) Update(ctx context.Context, userID, id int64, in UpdateAccountInput) (*model.Account, error) {
	a, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	// Populate the derived Currency from the asset so the "currency changed?"
	// check below and the response reflect the existing value (#284).
	if err := s.hydrateCurrency(ctx, a); err != nil {
		return nil, err
	}

	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.InstitutionSlug != nil {
		a.InstitutionSlug = strings.TrimSpace(*in.InstitutionSlug)
	}
	if in.AccountType != nil {
		a.AccountType = strings.TrimSpace(*in.AccountType)
	}
	if in.Currency != nil {
		newCurrency := strings.ToUpper(strings.TrimSpace(*in.Currency))
		if newCurrency != a.Currency {
			asset, err := s.assetRepo.EnsureBySymbolKind(ctx, newCurrency, model.AssetKindFiat, newCurrency)
			if err != nil {
				return nil, fmt.Errorf("resolve currency asset: %w", err)
			}
			a.Currency = newCurrency
			a.PrimaryQuoteAssetID = asset.ID
		}
	}
	if in.LastFour != nil {
		// Pointer to empty string means "clear it"; pass null pointer to leave alone.
		if *in.LastFour == "" {
			a.LastFour = nil
		} else {
			lf := *in.LastFour
			a.LastFour = &lf
		}
	}
	if in.IsActive != nil {
		a.IsActive = *in.IsActive
	}

	if err := validate(a); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, a); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("update account: %w", err)
	}
	return a, nil
}

// GetResponse returns the enriched DTO for one account.
func (s *AccountService) GetResponse(ctx context.Context, userID, id int64) (*AccountResponse, error) {
	a, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	items, err := s.loadPlaidItems(ctx, userID)
	if err != nil {
		return nil, err
	}
	resp := s.buildResponse(a, items)
	if err := s.fillBalances(ctx, userID, []*AccountResponse{&resp}); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListResponse mirrors List but returns enriched DTOs.
func (s *AccountService) ListResponse(ctx context.Context, userID int64, f repository.AccountFilter) ([]AccountResponse, int64, error) {
	accounts, total, err := s.List(ctx, userID, f)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.loadPlaidItems(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AccountResponse, 0, len(accounts))
	ptrs := make([]*AccountResponse, 0, len(accounts))
	for i := range accounts {
		out = append(out, s.buildResponse(&accounts[i], items))
		ptrs = append(ptrs, &out[len(out)-1])
	}
	if err := s.fillBalances(ctx, userID, ptrs); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// fillBalances derives each response's Balance from positions × prices via
// the valuation service (#291). One ListByUserID query feeds every account;
// each account is valued in its own primary quote asset. A stale/missing
// price chain drops that position from the sum and flips BalanceComplete to
// false (#282) — never a silent $0 masquerading as the full value.
func (s *AccountService) fillBalances(ctx context.Context, userID int64, resps []*AccountResponse) error {
	if len(resps) == 0 {
		return nil
	}
	if s.valuationSvc == nil {
		// No valuation wiring (unit tests): mark the zero balance incomplete
		// so it reads as "unknown", not "empty account".
		for _, r := range resps {
			r.BalanceComplete = false
		}
		return nil
	}
	positions, err := s.positionRepo.ListByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("list positions for balances: %w", err)
	}
	byAccount := make(map[int64][]model.Position)
	for _, p := range positions {
		byAccount[p.AccountID] = append(byAccount[p.AccountID], p)
	}
	now := time.Now().UTC()
	for _, r := range resps {
		val, err := s.valuationSvc.ValuePositions(ctx, byAccount[r.ID], now, r.PrimaryQuoteAssetID)
		if err != nil {
			return fmt.Errorf("value account %d: %w", r.ID, err)
		}
		r.Balance = val.Value
		r.BalanceComplete = val.Complete()
	}
	return nil
}

// loadPlaidItems indexes the user's PlaidItem rows by plaid_item_id (the
// Plaid-issued string ID stored on accounts.plaid_item_id). Returns nil
// when this instance has no Plaid wiring — caller handles that as "no
// sync status on any account".
func (s *AccountService) loadPlaidItems(ctx context.Context, userID int64) (map[string]*model.PlaidItem, error) {
	if s.plaidItemRepo == nil {
		return nil, nil
	}
	items, err := s.plaidItemRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load plaid items: %w", err)
	}
	out := make(map[string]*model.PlaidItem, len(items))
	for i := range items {
		out[items[i].PlaidItemID] = &items[i]
	}
	return out, nil
}

func (s *AccountService) buildResponse(a *model.Account, items map[string]*model.PlaidItem) AccountResponse {
	resp := AccountResponse{Account: a}
	if a.PlaidItemID == nil || items == nil {
		return resp
	}
	item, ok := items[*a.PlaidItemID]
	if !ok {
		return resp
	}
	status := item.LastSyncStatus
	resp.LastSyncStatus = &status
	if item.LastSyncedAt != nil {
		t := *item.LastSyncedAt
		resp.LastSyncedAt = &t
	}
	if item.LastSyncError != nil {
		resp.LastSyncError = item.LastSyncError
	}
	return resp
}

func (s *AccountService) SoftDelete(ctx context.Context, userID, id int64) error {
	if err := s.repo.SoftDelete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrAccountNotFound
		}
		return fmt.Errorf("soft delete account: %w", err)
	}
	return nil
}

func validate(a *model.Account) error {
	if strings.TrimSpace(a.Name) == "" {
		return ErrEmptyName
	}
	if strings.TrimSpace(a.InstitutionSlug) == "" {
		return ErrEmptyInstitution
	}
	if _, ok := validAccountTypes[a.AccountType]; !ok {
		return ErrInvalidType
	}
	if len(a.Currency) != 3 {
		return ErrInvalidCurrency
	}
	if a.LastFour != nil {
		v := *a.LastFour
		if len(v) != 4 {
			return ErrInvalidLastFour
		}
		for _, r := range v {
			if r < '0' || r > '9' {
				return ErrInvalidLastFour
			}
		}
	}
	return nil
}
