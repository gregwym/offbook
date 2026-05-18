package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
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

// CreateAccountInput is the validated, decoded request payload for account creation.
type CreateAccountInput struct {
	Name            string
	InstitutionSlug string
	AccountType     string
	Currency        string
	Balance         decimal.Decimal
	LastFour        *string
	IsActive        *bool
}

// UpdateAccountInput is a sparse patch. Pointer fields distinguish "not provided"
// from "set to zero value".
type UpdateAccountInput struct {
	Name            *string
	InstitutionSlug *string
	AccountType     *string
	Currency        *string
	Balance         *decimal.Decimal
	LastFour        *string
	IsActive        *bool
}

// AccountService owns business rules for accounts. It deliberately does NOT
// receive pii_repo or pii_service — PII is set via the separate pii endpoints.
// All operations are scoped to a user_id derived from the session.
//
// plaidItemRepo is an optional dependency used solely to join sync status
// into account response payloads (#65). Nil = no Plaid wiring on this
// instance; the response fields will be nil for every account.
type AccountService struct {
	repo          repository.AccountRepository
	plaidItemRepo repository.PlaidItemRepository
}

func NewAccountService(repo repository.AccountRepository) *AccountService {
	return &AccountService{repo: repo}
}

// WithPlaidItemRepo lets the router inject the optional dependency without
// breaking every caller of NewAccountService. Returns the service so the
// construction stays a one-liner.
func (s *AccountService) WithPlaidItemRepo(p repository.PlaidItemRepository) *AccountService {
	s.plaidItemRepo = p
	return s
}

func (s *AccountService) Create(ctx context.Context, userID int64, in CreateAccountInput) (*model.Account, error) {
	if in.Currency == "" {
		in.Currency = "USD"
	}
	a := &model.Account{
		UserID:          userID,
		Name:            strings.TrimSpace(in.Name),
		InstitutionSlug: strings.TrimSpace(in.InstitutionSlug),
		AccountType:     strings.TrimSpace(in.AccountType),
		Currency:        strings.ToUpper(strings.TrimSpace(in.Currency)),
		Balance:         in.Balance,
		LastFour:        in.LastFour,
		IsActive:        true,
	}
	if in.IsActive != nil {
		a.IsActive = *in.IsActive
	}
	if err := validate(a); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create account: %w", err)
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
	return a, nil
}

func (s *AccountService) List(ctx context.Context, userID int64, f repository.AccountFilter) ([]model.Account, int64, error) {
	return s.repo.List(ctx, userID, f)
}

func (s *AccountService) Update(ctx context.Context, userID, id int64, in UpdateAccountInput) (*model.Account, error) {
	a, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
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
		a.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if in.Balance != nil {
		a.Balance = *in.Balance
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
	for i := range accounts {
		out = append(out, s.buildResponse(&accounts[i], items))
	}
	return out, total, nil
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
	resp.LastSyncedAt = item.LastSyncedAt
	resp.LastSyncError = item.LastSyncError
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
	if a.Name == "" {
		return ErrEmptyName
	}
	if a.InstitutionSlug == "" {
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
