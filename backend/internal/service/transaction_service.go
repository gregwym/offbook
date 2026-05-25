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
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// Domain errors for transactions.
var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrInvalidCategory     = errors.New("invalid category")
	ErrInvalidAmount       = errors.New("amount must not be zero")
	ErrInvalidSource       = errors.New("invalid source")
	ErrMissingDate         = errors.New("transaction_date is required")
)

// validSources mirrors the CHECK constraint in migration 000001.
var validSources = map[string]struct{}{
	"manual": {},
	"plaid":  {},
	"csv":    {},
	"pdf":    {},
}

// CreateTransactionInput is the validated, decoded create payload.
// Source defaults to "manual" if empty. The transaction's asset is derived
// from the parent account's primary_quote_asset — cash-transaction-only in
// this PR; trade-pair entry (#238) will let callers override the asset.
type CreateTransactionInput struct {
	AccountID       int64
	CategoryID      *int64
	Amount          decimal.Decimal
	Description     *string
	MerchantName    *string
	TransactionDate time.Time
	PostedDate      *time.Time
	Source          string
	Notes           *string
}

// UpdateTransactionInput is a sparse patch. Per ADR-0013, asset_id on an
// existing transaction is not user-mutable — to "change the currency" of a
// transaction, delete it and re-enter under a different account.
type UpdateTransactionInput struct {
	CategoryID      *int64
	ClearCategory   bool
	Amount          *decimal.Decimal
	Description     *string
	MerchantName    *string
	TransactionDate *time.Time
	PostedDate      *time.Time
	Notes           *string
}

// TransactionService owns business rules for transactions.
// All read paths take the session user_id; cross-user reads are impossible.
type TransactionService struct {
	repo         repository.TransactionRepository
	accountRepo  repository.AccountRepository
	categoryRepo repository.CategoryRepository
	// ruleRepo lets Create apply the user's categorization rules when the
	// caller didn't supply a CategoryID. Optional — when nil, manual rows
	// land uncategorized (the M2-era behavior).
	ruleRepo repository.CategorizationRuleRepository
}

func NewTransactionService(
	repo repository.TransactionRepository,
	accountRepo repository.AccountRepository,
	categoryRepo repository.CategoryRepository,
) *TransactionService {
	return &TransactionService{repo: repo, accountRepo: accountRepo, categoryRepo: categoryRepo}
}

// WithRuleRepo wires the user-rule repository so Create applies rules to
// uncategorized inserts. Returns the receiver for one-liner construction.
func (s *TransactionService) WithRuleRepo(r repository.CategorizationRuleRepository) *TransactionService {
	s.ruleRepo = r
	return s
}

func (s *TransactionService) Create(ctx context.Context, userID int64, in CreateTransactionInput) (*model.Transaction, error) {
	src := strings.TrimSpace(in.Source)
	if src == "" {
		src = "manual"
	}
	if _, ok := validSources[src]; !ok {
		return nil, ErrInvalidSource
	}
	if in.Amount.IsZero() {
		return nil, ErrInvalidAmount
	}
	if in.TransactionDate.IsZero() {
		return nil, ErrMissingDate
	}
	// Fetch the account against the session user — this rejects transactions
	// targeting someone else's account and gives us the asset_id for the row.
	account, err := s.fetchOwnedAccount(ctx, userID, in.AccountID)
	if err != nil {
		return nil, err
	}
	if in.CategoryID != nil {
		if err := s.assertCategoryExists(ctx, *in.CategoryID); err != nil {
			return nil, err
		}
	}

	t := &model.Transaction{
		UserID:          userID,
		AccountID:       in.AccountID,
		AssetID:         account.PrimaryQuoteAssetID,
		CategoryID:      in.CategoryID,
		Amount:          in.Amount,
		Description:     trimPtr(in.Description),
		MerchantName:    trimPtr(in.MerchantName),
		TransactionDate: in.TransactionDate,
		PostedDate:      in.PostedDate,
		Source:          src,
		Notes:           in.Notes,
	}
	if in.CategoryID != nil {
		method := "manual"
		t.CategorizationMethod = &method
	} else if s.ruleRepo != nil {
		// No user-picked category — try the user's rules. We load and
		// compile per-Create because manual-entry volume is low (a few
		// rows per day per user); the cost is negligible and avoids
		// stale-cache bugs when the user has just edited a rule.
		rules, err := s.ruleRepo.List(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("load rules: %w", err)
		}
		categorization.Apply(t, categorization.Compile(rules))
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}
	return t, nil
}

func (s *TransactionService) List(ctx context.Context, userID int64, f repository.TransactionFilter) ([]model.Transaction, int64, error) {
	return s.repo.List(ctx, userID, f)
}

func (s *TransactionService) Get(ctx context.Context, userID, id int64) (*model.Transaction, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	return t, nil
}

func (s *TransactionService) Update(ctx context.Context, userID, id int64, in UpdateTransactionInput) (*model.Transaction, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}

	switch {
	case in.ClearCategory:
		t.CategoryID = nil
		t.CategorizationMethod = nil
	case in.CategoryID != nil:
		if err := s.assertCategoryExists(ctx, *in.CategoryID); err != nil {
			return nil, err
		}
		t.CategoryID = in.CategoryID
		method := "manual"
		t.CategorizationMethod = &method
	}

	if in.Amount != nil {
		if in.Amount.IsZero() {
			return nil, ErrInvalidAmount
		}
		t.Amount = *in.Amount
	}
	if in.Description != nil {
		t.Description = trimPtr(in.Description)
	}
	if in.MerchantName != nil {
		t.MerchantName = trimPtr(in.MerchantName)
	}
	if in.TransactionDate != nil {
		if in.TransactionDate.IsZero() {
			return nil, ErrMissingDate
		}
		t.TransactionDate = *in.TransactionDate
	}
	if in.PostedDate != nil {
		if in.PostedDate.IsZero() {
			t.PostedDate = nil
		} else {
			pd := *in.PostedDate
			t.PostedDate = &pd
		}
	}
	if in.Notes != nil {
		t.Notes = in.Notes
	}

	if err := s.repo.Update(ctx, t); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("update transaction: %w", err)
	}
	return t, nil
}

func (s *TransactionService) SoftDelete(ctx context.Context, userID, id int64) error {
	if err := s.repo.SoftDelete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTransactionNotFound
		}
		return fmt.Errorf("soft delete transaction: %w", err)
	}
	return nil
}

func (s *TransactionService) fetchOwnedAccount(ctx context.Context, userID, accountID int64) (*model.Account, error) {
	a, err := s.accountRepo.GetByID(ctx, userID, accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	return a, nil
}

func (s *TransactionService) assertCategoryExists(ctx context.Context, id int64) error {
	if _, err := s.categoryRepo.GetByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrInvalidCategory
		}
		return err
	}
	return nil
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}
