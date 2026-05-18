package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// Period names mirror the CHECK constraint on budgets.period (migration
// 000001). Keep these in sync if the schema changes.
const (
	BudgetPeriodMonthly = "monthly"
	BudgetPeriodWeekly  = "weekly"
	BudgetPeriodAnnual  = "annual"
)

var validBudgetPeriods = map[string]struct{}{
	BudgetPeriodMonthly: {},
	BudgetPeriodWeekly:  {},
	BudgetPeriodAnnual:  {},
}

var (
	ErrBudgetNotFound        = errors.New("budget not found")
	ErrInvalidBudgetPeriod   = errors.New("period must be one of: monthly, weekly, annual")
	ErrInvalidBudgetAmount   = errors.New("amount must be > 0")
	ErrDuplicateActiveBudget = errors.New("an active budget already exists for this user, category, and period")
)

// CreateBudgetInput is the validated payload for budget creation.
type CreateBudgetInput struct {
	CategoryID int64
	Period     string
	Amount     decimal.Decimal
	Rollover   *bool
	IsActive   *bool
}

// UpdateBudgetInput is a sparse patch.
type UpdateBudgetInput struct {
	CategoryID *int64
	Period     *string
	Amount     *decimal.Decimal
	Rollover   *bool
	IsActive   *bool
}

// BudgetSpend is the computed view of a budget against the current period's
// transactions. Spent is a positive decimal (the codebase's sign convention
// stores outflows as negative amounts; we flip the sign at the boundary).
type BudgetSpend struct {
	BudgetID    int64           `json:"budget_id"`
	CategoryID  int64           `json:"category_id"`
	Period      string          `json:"period"`
	PeriodStart time.Time       `json:"period_start"`
	PeriodEnd   time.Time       `json:"period_end"` // exclusive
	Limit       decimal.Decimal `json:"limit"`
	Spent       decimal.Decimal `json:"spent"`
	Remaining   decimal.Decimal `json:"remaining"`
	Pct         float64         `json:"pct"` // 0.0 — 1.0+ (uncapped; can exceed 1 when over budget)
}

// BudgetService owns budget validation, CRUD, and current-period spend
// calculation. now() is injectable so tests can drive period boundaries
// deterministically.
type BudgetService struct {
	repo    repository.BudgetRepository
	catRepo repository.CategoryRepository
	now     func() time.Time
}

func NewBudgetService(repo repository.BudgetRepository, catRepo repository.CategoryRepository) *BudgetService {
	return &BudgetService{repo: repo, catRepo: catRepo, now: time.Now}
}

// WithNow overrides the clock for tests. Returns the receiver for chaining.
func (s *BudgetService) WithNow(now func() time.Time) *BudgetService {
	s.now = now
	return s
}

func (s *BudgetService) Create(ctx context.Context, userID int64, in CreateBudgetInput) (*model.Budget, error) {
	period := strings.TrimSpace(in.Period)
	if _, ok := validBudgetPeriods[period]; !ok {
		return nil, ErrInvalidBudgetPeriod
	}
	if !in.Amount.IsPositive() {
		return nil, ErrInvalidBudgetAmount
	}
	if _, err := s.catRepo.GetByID(ctx, in.CategoryID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUnknownCategory
		}
		return nil, err
	}
	b := &model.Budget{
		UserID:     userID,
		CategoryID: in.CategoryID,
		Period:     period,
		Amount:     in.Amount,
		Rollover:   in.Rollover != nil && *in.Rollover,
		IsActive:   true,
	}
	if in.IsActive != nil {
		b.IsActive = *in.IsActive
	}
	if err := s.repo.Create(ctx, b); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateActiveBudget
		}
		return nil, fmt.Errorf("create budget: %w", err)
	}
	return b, nil
}

func (s *BudgetService) Get(ctx context.Context, userID, id int64) (*model.Budget, error) {
	b, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, err
	}
	return b, nil
}

func (s *BudgetService) List(ctx context.Context, userID int64) ([]model.Budget, error) {
	return s.repo.List(ctx, userID)
}

func (s *BudgetService) Update(ctx context.Context, userID, id int64, in UpdateBudgetInput) (*model.Budget, error) {
	b, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, err
	}
	if in.CategoryID != nil {
		if _, err := s.catRepo.GetByID(ctx, *in.CategoryID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, ErrUnknownCategory
			}
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
	if err := s.repo.Update(ctx, b); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		if isUniqueViolation(err) {
			return nil, ErrDuplicateActiveBudget
		}
		return nil, fmt.Errorf("update budget: %w", err)
	}
	return b, nil
}

func (s *BudgetService) SoftDelete(ctx context.Context, userID, id int64) error {
	if err := s.repo.SoftDelete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrBudgetNotFound
		}
		return fmt.Errorf("soft delete budget: %w", err)
	}
	return nil
}

// Spend returns the current-period spend view for a budget. The period
// window depends on the budget's `period` field; see budgetPeriodWindow.
func (s *BudgetService) Spend(ctx context.Context, userID, id int64) (*BudgetSpend, error) {
	b, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	from, to := budgetPeriodWindow(b.Period, s.now())
	spent, err := s.repo.CurrentPeriodSpend(ctx, userID, b.CategoryID, from, to)
	if err != nil {
		return nil, fmt.Errorf("compute spend: %w", err)
	}
	remaining := b.Amount.Sub(spent)
	pct := 0.0
	if b.Amount.IsPositive() {
		p, _ := spent.Div(b.Amount).Float64()
		pct = p
	}
	return &BudgetSpend{
		BudgetID:    b.ID,
		CategoryID:  b.CategoryID,
		Period:      b.Period,
		PeriodStart: from,
		PeriodEnd:   to,
		Limit:       b.Amount,
		Spent:       spent,
		Remaining:   remaining,
		Pct:         pct,
	}, nil
}

// budgetPeriodWindow returns [from, to) for the budget's current period
// relative to `now`. UTC throughout — budgets are user-private and a
// stable definition beats per-user time-zone gymnastics.
//
//	monthly: first → first of next month
//	weekly:  ISO week (Monday-start) → Monday of the following week
//	annual:  Jan 1 → Jan 1 of next year
func budgetPeriodWindow(period string, now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	switch period {
	case BudgetPeriodWeekly:
		// Monday-start ISO week. Go's Weekday returns Sunday=0..Saturday=6;
		// shift so Monday=0..Sunday=6.
		wd := int(now.Weekday()+6) % 7
		monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -wd)
		return monday, monday.AddDate(0, 0, 7)
	case BudgetPeriodAnnual:
		from := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		return from, from.AddDate(1, 0, 0)
	default: // monthly (and any unknown value — caller should have validated)
		from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return from, from.AddDate(0, 1, 0)
	}
}

// isUniqueViolation matches the Postgres "unique_violation" SQLSTATE 23505
// — the only path the budgets repo can hit it on is the partial unique
// index on (user_id, category_id, period) WHERE deleted_at IS NULL AND
// is_active = TRUE (migration 000001).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
