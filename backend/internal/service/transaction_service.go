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

// Domain errors for transactions.
var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrInvalidCategory     = errors.New("invalid category")
	ErrInvalidAmount       = errors.New("amount must not be zero")
	ErrInvalidSource       = errors.New("invalid source")
	ErrMissingDate         = errors.New("transaction_date is required")
)

// validSources mirrors the CHECK constraint in migration 000001.
// Manual entry uses 'manual'; the other values exist for future ingestion paths.
var validSources = map[string]struct{}{
	"manual": {},
	"plaid":  {},
	"csv":    {},
	"pdf":    {},
}

// CreateTransactionInput is the validated, decoded create payload.
// Source defaults to "manual" if empty.
type CreateTransactionInput struct {
	AccountID       int64
	CategoryID      *int64
	Amount          decimal.Decimal
	Currency        string
	Description     *string
	MerchantName    *string
	TransactionDate time.Time
	PostedDate      *time.Time
	Source          string
	Notes           *string
}

// UpdateTransactionInput is a sparse patch. Pointer fields distinguish
// "not provided" from "set to zero value".
type UpdateTransactionInput struct {
	CategoryID      *int64           // pointer-to-pointer omitted; pass *id to set, nil-but-Provided not supported in this minimal patch — see ClearCategory
	ClearCategory   bool             // true uncategorizes the row
	Amount          *decimal.Decimal
	Currency        *string
	Description     *string
	MerchantName    *string
	TransactionDate *time.Time
	PostedDate      *time.Time
	Notes           *string
}

// TransactionService owns business rules for transactions. It depends on the
// account and category repos only for existence checks — it never reads PII.
type TransactionService struct {
	repo        repository.TransactionRepository
	accountRepo repository.AccountRepository
	categoryRepo repository.CategoryRepository
}

func NewTransactionService(
	repo repository.TransactionRepository,
	accountRepo repository.AccountRepository,
	categoryRepo repository.CategoryRepository,
) *TransactionService {
	return &TransactionService{repo: repo, accountRepo: accountRepo, categoryRepo: categoryRepo}
}

func (s *TransactionService) Create(ctx context.Context, in CreateTransactionInput) (*model.Transaction, error) {
	// Source defaulting + validation. Empty == manual per the issue's "manual entry" framing.
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
	if err := s.assertAccountExists(ctx, in.AccountID); err != nil {
		return nil, err
	}
	if in.CategoryID != nil {
		if err := s.assertCategoryExists(ctx, *in.CategoryID); err != nil {
			return nil, err
		}
	}

	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USD"
	}

	t := &model.Transaction{
		AccountID:       in.AccountID,
		CategoryID:      in.CategoryID,
		Amount:          in.Amount,
		Currency:        currency,
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
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}
	return t, nil
}

// List returns transactions matching the filter, plus the total count
// (count uses the same WHERE clause sans limit/offset so the UI can render
// pagination controls).
func (s *TransactionService) List(ctx context.Context, f repository.TransactionFilter) ([]model.Transaction, int64, error) {
	return s.repo.List(ctx, f)
}

func (s *TransactionService) Get(ctx context.Context, id int64) (*model.Transaction, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	return t, nil
}

func (s *TransactionService) Update(ctx context.Context, id int64, in UpdateTransactionInput) (*model.Transaction, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}

	// Category handling: caller can either set (CategoryID != nil) or clear (ClearCategory = true).
	// Setting + clearing in the same patch is contradictory — clear wins.
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
	if in.Currency != nil {
		c := strings.ToUpper(strings.TrimSpace(*in.Currency))
		if len(c) != 3 {
			return nil, ErrInvalidCurrency
		}
		t.Currency = c
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
		// Empty time means "clear" so the caller can unset a previously-set posted_date.
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

func (s *TransactionService) SoftDelete(ctx context.Context, id int64) error {
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTransactionNotFound
		}
		return fmt.Errorf("soft delete transaction: %w", err)
	}
	return nil
}

func (s *TransactionService) assertAccountExists(ctx context.Context, id int64) error {
	if _, err := s.accountRepo.GetByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrAccountNotFound
		}
		return err
	}
	return nil
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
