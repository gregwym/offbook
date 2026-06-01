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

var (
	ErrSavingsGoalNotFound = errors.New("savings goal not found")
	ErrEmptyGoalName       = errors.New("name must not be empty")
	ErrInvalidTargetAmount = errors.New("target_amount must be > 0")
	ErrZeroContribution    = errors.New("contribution amount must not be zero")
	ErrGoalAccountMismatch = errors.New("linked account does not belong to this user")
)

// CreateGoalInput is the validated payload for goal creation.
type CreateGoalInput struct {
	Name         string
	TargetAmount decimal.Decimal
	TargetDate   *time.Time
	AccountID    *int64
}

// UpdateGoalInput is a sparse patch. ClearAccountID=true clears the link.
type UpdateGoalInput struct {
	Name            *string
	TargetAmount    *decimal.Decimal
	TargetDate      *time.Time
	ClearTargetDate bool
	AccountID       *int64
	ClearAccountID  bool
}

// GoalView pairs the persisted row with a computed progress percentage
// (0.0–1.0, capped) and a remaining amount (>= 0).
type GoalView struct {
	*model.SavingsGoal
	ProgressPct float64         `json:"progress_pct"`
	Remaining   decimal.Decimal `json:"remaining"`
}

// SavingsGoalService owns goal validation and CRUD + atomic contributions.
type SavingsGoalService struct {
	repo     repository.SavingsGoalRepository
	acctRepo repository.AccountRepository
}

func NewSavingsGoalService(repo repository.SavingsGoalRepository, acctRepo repository.AccountRepository) *SavingsGoalService {
	return &SavingsGoalService{repo: repo, acctRepo: acctRepo}
}

func (s *SavingsGoalService) Create(ctx context.Context, owner repository.PlanOwner, in CreateGoalInput) (*model.SavingsGoal, error) {
	g := &model.SavingsGoal{
		UserID:        owner.UserID,
		HouseholdID:   owner.HouseholdID,
		Name:          strings.TrimSpace(in.Name),
		TargetAmount:  in.TargetAmount,
		CurrentAmount: decimal.Zero,
		TargetDate:    in.TargetDate,
		AccountID:     in.AccountID,
	}
	if err := s.validate(ctx, g); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("create goal: %w", err)
	}
	return g, nil
}

func (s *SavingsGoalService) Get(ctx context.Context, owner repository.PlanOwner, id int64) (*model.SavingsGoal, error) {
	g, err := s.repo.GetByID(ctx, owner, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSavingsGoalNotFound
		}
		return nil, err
	}
	return g, nil
}

func (s *SavingsGoalService) List(ctx context.Context, owner repository.PlanOwner) ([]model.SavingsGoal, error) {
	return s.repo.List(ctx, owner)
}

func (s *SavingsGoalService) Update(ctx context.Context, owner repository.PlanOwner, id int64, in UpdateGoalInput) (*model.SavingsGoal, error) {
	g, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		g.Name = strings.TrimSpace(*in.Name)
	}
	if in.TargetAmount != nil {
		g.TargetAmount = *in.TargetAmount
	}
	if in.ClearTargetDate {
		g.TargetDate = nil
	} else if in.TargetDate != nil {
		td := *in.TargetDate
		g.TargetDate = &td
	}
	if in.ClearAccountID {
		g.AccountID = nil
	} else if in.AccountID != nil {
		id := *in.AccountID
		g.AccountID = &id
	}
	if err := s.validate(ctx, g); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, g); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSavingsGoalNotFound
		}
		return nil, fmt.Errorf("update goal: %w", err)
	}
	return g, nil
}

func (s *SavingsGoalService) SoftDelete(ctx context.Context, owner repository.PlanOwner, id int64) error {
	if err := s.repo.SoftDelete(ctx, owner, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrSavingsGoalNotFound
		}
		return fmt.Errorf("soft delete goal: %w", err)
	}
	return nil
}

// Contribute applies delta (positive = deposit, negative = withdrawal)
// atomically. Returns the post-update row. delta=0 is rejected because
// it's almost always a bug in the caller.
func (s *SavingsGoalService) Contribute(ctx context.Context, owner repository.PlanOwner, id int64, delta decimal.Decimal) (*model.SavingsGoal, error) {
	if delta.IsZero() {
		return nil, ErrZeroContribution
	}
	g, err := s.repo.AddContribution(ctx, owner, id, delta)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSavingsGoalNotFound
		}
		return nil, fmt.Errorf("add contribution: %w", err)
	}
	return g, nil
}

// View enriches a goal with its computed progress + remaining.
func View(g *model.SavingsGoal) GoalView {
	v := GoalView{SavingsGoal: g, Remaining: decimal.Zero, ProgressPct: 0}
	if g == nil {
		return v
	}
	if g.TargetAmount.IsPositive() {
		p, _ := g.CurrentAmount.Div(g.TargetAmount).Float64()
		if p > 1 {
			p = 1
		}
		if p < 0 {
			p = 0
		}
		v.ProgressPct = p
	}
	rem := g.TargetAmount.Sub(g.CurrentAmount)
	if rem.IsNegative() {
		rem = decimal.Zero
	}
	v.Remaining = rem
	return v
}

func (s *SavingsGoalService) validate(ctx context.Context, g *model.SavingsGoal) error {
	if g.Name == "" {
		return ErrEmptyGoalName
	}
	if !g.TargetAmount.IsPositive() {
		return ErrInvalidTargetAmount
	}
	if g.AccountID != nil {
		// account_id is only meaningful for a personal goal (DB CHECK
		// savings_goals_account_personal_chk). A household goal with an
		// account link is a caller bug.
		if g.UserID == nil {
			return ErrGoalAccountMismatch
		}
		// Owning user must match — block cross-user account linkage.
		if _, err := s.acctRepo.GetByID(ctx, *g.UserID, *g.AccountID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrGoalAccountMismatch
			}
			return fmt.Errorf("validate account: %w", err)
		}
	}
	return nil
}
