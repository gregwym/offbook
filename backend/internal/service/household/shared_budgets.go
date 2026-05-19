package household

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// SharedBudgetInput is the validated payload for creating a shared budget.
// Same shape as the personal-budget input minus user_id (household_id is
// derived from the path).
type SharedBudgetInput struct {
	CategoryID int64
	Period     string
	Amount     decimal.Decimal
	Rollover   *bool
	IsActive   *bool
}

// UpdateSharedBudgetInput is a sparse patch.
type UpdateSharedBudgetInput struct {
	CategoryID *int64
	Period     *string
	Amount     *decimal.Decimal
	Rollover   *bool
	IsActive   *bool
}

// Shared-budget errors complement the household-wide ones in errors.go.
// We intentionally don't reuse the personal-budget errors (different
// package boundary, simpler mapping in the handler).
var (
	ErrInvalidBudgetPeriod = errors.New("period must be one of: monthly, weekly, annual")
	ErrInvalidBudgetAmount = errors.New("amount must be > 0")
	ErrUnknownCategory     = errors.New("category does not exist")
	ErrBudgetNotFound      = errors.New("shared budget not found")
)

var validBudgetPeriods = map[string]struct{}{
	"monthly": {}, "weekly": {}, "annual": {},
}

// WithSharedBudgets wires the optional repos that the shared-budget CRUD
// path needs. Returns the receiver so it composes with WithDB.
func (s *Service) WithSharedBudgets(repo repository.SharedBudgetRepository, categories repository.CategoryRepository) *Service {
	s.sharedBudgets = repo
	s.categories = categories
	return s
}

// CreateSharedBudget creates a budget owned by the household. Owner or
// contributor may create; view_only is read-only.
func (s *Service) CreateSharedBudget(ctx context.Context, userID, householdID int64, in SharedBudgetInput) (*model.SharedBudget, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	period := strings.TrimSpace(in.Period)
	if _, ok := validBudgetPeriods[period]; !ok {
		return nil, ErrInvalidBudgetPeriod
	}
	if !in.Amount.IsPositive() {
		return nil, ErrInvalidBudgetAmount
	}
	if err := s.requireCategoryExists(ctx, in.CategoryID); err != nil {
		return nil, err
	}
	b := &model.SharedBudget{
		HouseholdID: householdID,
		CategoryID:  in.CategoryID,
		Period:      period,
		Amount:      in.Amount,
		Rollover:    in.Rollover != nil && *in.Rollover,
		IsActive:    true,
	}
	if in.IsActive != nil {
		b.IsActive = *in.IsActive
	}
	if err := s.sharedBudgets.Create(ctx, b); err != nil {
		return nil, fmt.Errorf("create shared_budget: %w", err)
	}
	return b, nil
}

// ListSharedBudgets returns every shared budget for the household. Any
// active member may read (matches the read-side aggregator's audience).
func (s *Service) ListSharedBudgets(ctx context.Context, userID, householdID int64) ([]model.SharedBudget, error) {
	if _, err := s.members.GetActive(ctx, householdID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotMember
		}
		return nil, err
	}
	return s.sharedBudgets.List(ctx, householdID)
}

// UpdateSharedBudget applies a sparse patch. Owner or contributor may edit.
func (s *Service) UpdateSharedBudget(ctx context.Context, userID, householdID, id int64, in UpdateSharedBudgetInput) (*model.SharedBudget, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	b, err := s.sharedBudgets.GetByID(ctx, householdID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, err
	}
	if in.CategoryID != nil {
		if err := s.requireCategoryExists(ctx, *in.CategoryID); err != nil {
			return nil, err
		}
		b.CategoryID = *in.CategoryID
	}
	if in.Period != nil {
		p := strings.TrimSpace(*in.Period)
		if _, ok := validBudgetPeriods[p]; !ok {
			return nil, ErrInvalidBudgetPeriod
		}
		b.Period = p
	}
	if in.Amount != nil {
		if !in.Amount.IsPositive() {
			return nil, ErrInvalidBudgetAmount
		}
		b.Amount = *in.Amount
	}
	if in.Rollover != nil {
		b.Rollover = *in.Rollover
	}
	if in.IsActive != nil {
		b.IsActive = *in.IsActive
	}
	if err := s.sharedBudgets.Update(ctx, b); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, fmt.Errorf("update shared_budget: %w", err)
	}
	return b, nil
}

// SoftDeleteSharedBudget removes a budget. Owner or contributor may delete.
func (s *Service) SoftDeleteSharedBudget(ctx context.Context, userID, householdID, id int64) error {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return err
	}
	if err := s.sharedBudgets.SoftDelete(ctx, householdID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrBudgetNotFound
		}
		return fmt.Errorf("soft-delete shared_budget: %w", err)
	}
	return nil
}

// requireContributor accepts owner OR contributor; view_only is rejected
// with ErrForbidden. Non-members get ErrNotMember.
func (s *Service) requireContributor(ctx context.Context, userID, householdID int64) error {
	mem, err := s.members.GetActive(ctx, householdID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotMember
		}
		return err
	}
	switch mem.Role {
	case model.RoleOwner, model.RoleContributor:
		return nil
	default:
		return ErrForbidden
	}
}

// requireCategoryExists is a thin guard so we surface a clean error code
// instead of a foreign-key violation when the caller passes a bad id.
func (s *Service) requireCategoryExists(ctx context.Context, categoryID int64) error {
	if s.categories == nil {
		// Defensive: if WithSharedBudgets wasn't called we'd hit the FK
		// error path below — but the cleaner thing is to fail early.
		return ErrUnknownCategory
	}
	if _, err := s.categories.GetByID(ctx, categoryID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrUnknownCategory
		}
		return fmt.Errorf("validate category: %w", err)
	}
	return nil
}
