package household

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

// SharedGoalInput is the validated payload for creating a shared goal.
type SharedGoalInput struct {
	Name         string
	TargetAmount decimal.Decimal
	TargetDate   *time.Time
}

// UpdateSharedGoalInput is a sparse patch. ClearTargetDate=true removes
// the existing date — mirrors the personal SavingsGoal API.
type UpdateSharedGoalInput struct {
	Name            *string
	TargetAmount    *decimal.Decimal
	TargetDate      *time.Time
	ClearTargetDate bool
}

// Shared-goal errors complement the household-wide ones in errors.go.
var (
	ErrSharedGoalNotFound         = errors.New("shared goal not found")
	ErrSharedGoalEmptyName        = errors.New("name must not be empty")
	ErrSharedGoalInvalidTarget    = errors.New("target_amount must be > 0")
	ErrSharedGoalZeroContribution = errors.New("contribution amount must not be zero")
)

// WithSharedGoals wires the optional repo that the shared-goal CRUD path
// needs. Returns the receiver so it composes with WithDB / WithSharedBudgets.
func (s *Service) WithSharedGoals(repo repository.SharedGoalRepository) *Service {
	s.sharedGoals = repo
	return s
}

// CreateSharedGoal creates a goal owned by the household. Owner or
// contributor may create.
func (s *Service) CreateSharedGoal(ctx context.Context, userID, householdID int64, in SharedGoalInput) (*model.SharedGoal, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrSharedGoalEmptyName
	}
	if !in.TargetAmount.IsPositive() {
		return nil, ErrSharedGoalInvalidTarget
	}
	g := &model.SharedGoal{
		HouseholdID:   householdID,
		Name:          name,
		TargetAmount:  in.TargetAmount,
		CurrentAmount: decimal.Zero,
		TargetDate:    in.TargetDate,
	}
	if err := s.sharedGoals.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("create shared_goal: %w", err)
	}
	return g, nil
}

// ListSharedGoals returns every shared goal for the household. Any active
// member may read.
func (s *Service) ListSharedGoals(ctx context.Context, userID, householdID int64) ([]model.SharedGoal, error) {
	if _, err := s.members.GetActive(ctx, householdID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotMember
		}
		return nil, err
	}
	return s.sharedGoals.List(ctx, householdID)
}

// UpdateSharedGoal applies a sparse patch. Owner or contributor may edit.
func (s *Service) UpdateSharedGoal(ctx context.Context, userID, householdID, id int64, in UpdateSharedGoalInput) (*model.SharedGoal, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	g, err := s.sharedGoals.GetByID(ctx, householdID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSharedGoalNotFound
		}
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, ErrSharedGoalEmptyName
		}
		g.Name = name
	}
	if in.TargetAmount != nil {
		if !in.TargetAmount.IsPositive() {
			return nil, ErrSharedGoalInvalidTarget
		}
		g.TargetAmount = *in.TargetAmount
	}
	if in.ClearTargetDate {
		g.TargetDate = nil
	} else if in.TargetDate != nil {
		td := *in.TargetDate
		g.TargetDate = &td
	}
	if err := s.sharedGoals.Update(ctx, g); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSharedGoalNotFound
		}
		return nil, fmt.Errorf("update shared_goal: %w", err)
	}
	return g, nil
}

// SoftDeleteSharedGoal removes a goal. Owner or contributor.
func (s *Service) SoftDeleteSharedGoal(ctx context.Context, userID, householdID, id int64) error {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return err
	}
	if err := s.sharedGoals.SoftDelete(ctx, householdID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrSharedGoalNotFound
		}
		return fmt.Errorf("soft-delete shared_goal: %w", err)
	}
	return nil
}

// ContributeToSharedGoal atomically adjusts current_amount by `delta`.
// Positive = deposit, negative = withdrawal. Zero is rejected as a likely
// caller bug (mirrors personal SavingsGoalService.Contribute).
func (s *Service) ContributeToSharedGoal(ctx context.Context, userID, householdID, id int64, delta decimal.Decimal) (*model.SharedGoal, error) {
	if err := s.requireContributor(ctx, userID, householdID); err != nil {
		return nil, err
	}
	if delta.IsZero() {
		return nil, ErrSharedGoalZeroContribution
	}
	g, err := s.sharedGoals.AddContribution(ctx, householdID, id, delta)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSharedGoalNotFound
		}
		return nil, fmt.Errorf("add contribution: %w", err)
	}
	return g, nil
}
