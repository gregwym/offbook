package household

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// SharedGoalInput is the validated payload for creating a household goal.
type SharedGoalInput struct {
	Name         string
	TargetAmount decimal.Decimal
	TargetDate   *time.Time
}

// UpdateSharedGoalInput is a sparse patch. ClearTargetDate=true removes the
// existing date — mirrors the personal SavingsGoal API.
type UpdateSharedGoalInput struct {
	Name            *string
	TargetAmount    *decimal.Decimal
	TargetDate      *time.Time
	ClearTargetDate bool
}

// Goal errors are aliases of the unified savings-goal service's errors
// (ADR-0018) so household and personal goals share one validated path and one
// error set. The handler's errors.Is checks keep working unchanged.
var (
	ErrSharedGoalNotFound         = service.ErrSavingsGoalNotFound
	ErrSharedGoalEmptyName        = service.ErrEmptyGoalName
	ErrSharedGoalInvalidTarget    = service.ErrInvalidTargetAmount
	ErrSharedGoalZeroContribution = service.ErrZeroContribution
)

// WithSharedGoals wires the unified savings-goal service used for
// household-owned goal CRUD. Returns the receiver so it composes with WithDB /
// WithSharedBudgets.
func (s *Service) WithSharedGoals(goals *service.SavingsGoalService) *Service {
	s.goals = goals
	return s
}

// CreateSharedGoal creates a goal owned by the household. Owner or contributor
// may create. A household goal spans members' shared accounts, so it never
// carries an account_id (enforced by the goal service + DB CHECK).
func (s *Service) CreateSharedGoal(ctx context.Context, userID, householdID int64, in SharedGoalInput) (*model.SavingsGoal, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	return s.goals.Create(ctx, repository.HouseholdOwner(householdID), service.CreateGoalInput{
		Name:         in.Name,
		TargetAmount: in.TargetAmount,
		TargetDate:   in.TargetDate,
	})
}

// ListSharedGoals returns every household-owned goal. Any active member may read.
func (s *Service) ListSharedGoals(ctx context.Context, userID, householdID int64) ([]model.SavingsGoal, error) {
	if _, err := s.members.GetActive(ctx, householdID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotMember
		}
		return nil, err
	}
	return s.goals.List(ctx, repository.HouseholdOwner(householdID))
}

// UpdateSharedGoal applies a sparse patch. Owner or contributor may edit.
func (s *Service) UpdateSharedGoal(ctx context.Context, userID, householdID, id int64, in UpdateSharedGoalInput) (*model.SavingsGoal, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	return s.goals.Update(ctx, repository.HouseholdOwner(householdID), id, service.UpdateGoalInput{
		Name:            in.Name,
		TargetAmount:    in.TargetAmount,
		TargetDate:      in.TargetDate,
		ClearTargetDate: in.ClearTargetDate,
	})
}

// SoftDeleteSharedGoal removes a goal. Owner or contributor.
func (s *Service) SoftDeleteSharedGoal(ctx context.Context, userID, householdID, id int64) error {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return err
	}
	return s.goals.SoftDelete(ctx, repository.HouseholdOwner(householdID), id)
}

// ContributeToSharedGoal atomically adjusts current_amount by `delta`.
// Positive = deposit, negative = withdrawal. Zero is rejected by the goal
// service as a likely caller bug.
func (s *Service) ContributeToSharedGoal(ctx context.Context, userID, householdID, id int64, delta decimal.Decimal) (*model.SavingsGoal, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	return s.goals.Contribute(ctx, repository.HouseholdOwner(householdID), id, delta)
}
