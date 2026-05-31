package household

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// SharedBudgetInput is the validated payload for creating a household budget.
// Same shape as the personal-budget input minus the owner (household_id is
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

// Budget errors are aliases of the unified budget service's errors (ADR-0018):
// household and personal budgets share one validated CRUD path, so they share
// one error set. The handler's errors.Is checks keep working unchanged.
var (
	ErrInvalidBudgetPeriod   = service.ErrInvalidBudgetPeriod
	ErrInvalidBudgetAmount   = service.ErrInvalidBudgetAmount
	ErrUnknownCategory       = service.ErrUnknownCategory
	ErrBudgetNotFound        = service.ErrBudgetNotFound
	ErrDuplicateActiveBudget = service.ErrDuplicateActiveBudget
)

// WithSharedBudgets wires the unified budget service used for household-owned
// budget CRUD. Returns the receiver so it composes with WithDB / WithSharedGoals.
func (s *Service) WithSharedBudgets(budgets *service.BudgetService) *Service {
	s.budgets = budgets
	return s
}

// CreateSharedBudget creates a budget owned by the household. Owner or
// contributor may create; view_only is read-only. The validation, category
// check, and duplicate-active detection live once in the budget service.
func (s *Service) CreateSharedBudget(ctx context.Context, userID, householdID int64, in SharedBudgetInput) (*model.Budget, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	return s.budgets.Create(ctx, repository.HouseholdOwner(householdID), service.CreateBudgetInput{
		CategoryID: in.CategoryID,
		Period:     in.Period,
		Amount:     in.Amount,
		Rollover:   in.Rollover,
		IsActive:   in.IsActive,
	})
}

// ListSharedBudgets returns every household-owned budget. Any active member
// may read (matches the read-side aggregator's audience).
func (s *Service) ListSharedBudgets(ctx context.Context, userID, householdID int64) ([]model.Budget, error) {
	if _, err := s.members.GetActive(ctx, householdID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotMember
		}
		return nil, err
	}
	return s.budgets.List(ctx, repository.HouseholdOwner(householdID))
}

// UpdateSharedBudget applies a sparse patch. Owner or contributor may edit.
func (s *Service) UpdateSharedBudget(ctx context.Context, userID, householdID, id int64, in UpdateSharedBudgetInput) (*model.Budget, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	return s.budgets.Update(ctx, repository.HouseholdOwner(householdID), id, service.UpdateBudgetInput{
		CategoryID: in.CategoryID,
		Period:     in.Period,
		Amount:     in.Amount,
		Rollover:   in.Rollover,
		IsActive:   in.IsActive,
	})
}

// SoftDeleteSharedBudget removes a budget. Owner or contributor may delete.
func (s *Service) SoftDeleteSharedBudget(ctx context.Context, userID, householdID, id int64) error {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return err
	}
	return s.budgets.SoftDelete(ctx, repository.HouseholdOwner(householdID), id)
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
