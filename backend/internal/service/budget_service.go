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
	ErrInvalidRolloverStart  = errors.New("rollover starting balance must be >= 0")
	ErrBudgetDateInFuture    = errors.New("budget period date must not be after today")
)

// CreateBudgetInput is the validated payload for budget creation.
type CreateBudgetInput struct {
	CategoryID int64
	Period     string
	Amount     decimal.Decimal
	Rollover   *bool
	// RolloverStart is the optional starting carryover (nil = 0).
	RolloverStart *decimal.Decimal
	IsActive      *bool
}

// UpdateBudgetInput is a sparse patch.
type UpdateBudgetInput struct {
	CategoryID    *int64
	Period        *string
	Amount        *decimal.Decimal
	Rollover      *bool
	RolloverStart *decimal.Decimal
	IsActive      *bool
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
	// Carryover is the unspent balance rolled in from earlier periods (always
	// >= 0; zero unless the budget has rollover on). Available = Limit +
	// Carryover; Remaining and Pct are measured against Available.
	Carryover decimal.Decimal `json:"carryover"`
	Available decimal.Decimal `json:"available"`
	Spent     decimal.Decimal `json:"spent"`
	Remaining decimal.Decimal `json:"remaining"`
	Pct       float64         `json:"pct"` // 0.0 — 1.0+ (uncapped; can exceed 1 when over budget)
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

func (s *BudgetService) Create(ctx context.Context, owner repository.PlanOwner, in CreateBudgetInput) (*model.Budget, error) {
	period := strings.TrimSpace(in.Period)
	if _, ok := validBudgetPeriods[period]; !ok {
		return nil, ErrInvalidBudgetPeriod
	}
	if !in.Amount.IsPositive() {
		return nil, ErrInvalidBudgetAmount
	}
	if in.RolloverStart != nil && in.RolloverStart.IsNegative() {
		return nil, ErrInvalidRolloverStart
	}
	// A personal budget may use the user's own categories; a household
	// budget only the shared system taxonomy (#573).
	if _, err := s.catRepo.GetVisible(ctx, owner.UserID, in.CategoryID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUnknownCategory
		}
		return nil, err
	}
	b := &model.Budget{
		UserID:      owner.UserID,
		HouseholdID: owner.HouseholdID,
		CategoryID:  in.CategoryID,
		Period:      period,
		Amount:      in.Amount,
		Rollover:    in.Rollover != nil && *in.Rollover,
		IsActive:    true,
	}
	if in.RolloverStart != nil {
		b.RolloverStart = *in.RolloverStart
	}
	if in.IsActive != nil {
		b.IsActive = *in.IsActive
	}
	from, _ := budgetPeriodWindow(b.Period, s.now())
	if err := s.repo.Create(ctx, b, from); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateActiveBudget
		}
		return nil, fmt.Errorf("create budget: %w", err)
	}
	return b, nil
}

func (s *BudgetService) Get(ctx context.Context, owner repository.PlanOwner, id int64) (*model.Budget, error) {
	b, err := s.repo.GetByID(ctx, owner, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, err
	}
	return b, nil
}

func (s *BudgetService) List(ctx context.Context, owner repository.PlanOwner) ([]model.Budget, error) {
	return s.repo.List(ctx, owner)
}

func (s *BudgetService) Update(ctx context.Context, owner repository.PlanOwner, id int64, in UpdateBudgetInput) (*model.Budget, error) {
	b, err := s.repo.GetByID(ctx, owner, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, err
	}
	if in.CategoryID != nil {
		if _, err := s.catRepo.GetVisible(ctx, owner.UserID, *in.CategoryID); err != nil {
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
	prevAmount := b.Amount
	if in.Amount != nil {
		if !in.Amount.IsPositive() {
			return nil, ErrInvalidBudgetAmount
		}
		b.Amount = *in.Amount
	}
	if in.Rollover != nil {
		b.Rollover = *in.Rollover
	}
	if in.RolloverStart != nil {
		if in.RolloverStart.IsNegative() {
			return nil, ErrInvalidRolloverStart
		}
		b.RolloverStart = *in.RolloverStart
	}
	if in.IsActive != nil {
		b.IsActive = *in.IsActive
	}
	// A new limit applies from the current period on; earlier periods keep
	// the limit they had (#580).
	var change *repository.BudgetLimitChange
	if !b.Amount.Equal(prevAmount) {
		from, _ := budgetPeriodWindow(b.Period, s.now())
		change = &repository.BudgetLimitChange{From: from, PrevFrom: prevPeriodStart(b.Period, from), Prev: prevAmount}
	}
	if err := s.repo.Update(ctx, b, change); err != nil {
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

func (s *BudgetService) SoftDelete(ctx context.Context, owner repository.PlanOwner, id int64) error {
	if err := s.repo.SoftDelete(ctx, owner, id); err != nil {
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
	return s.SpendOn(ctx, userID, id, s.now())
}

// SpendOn returns the spend view for the budget period containing `on`, so
// the Budgets page can step back to an earlier month (#577). A date after
// today is rejected. Each period is measured against the limit in effect
// then (#580).
func (s *BudgetService) SpendOn(ctx context.Context, userID, id int64, on time.Time) (*BudgetSpend, error) {
	now := s.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if !on.UTC().Before(today.AddDate(0, 0, 1)) {
		return nil, ErrBudgetDateInFuture
	}
	b, err := s.Get(ctx, repository.UserOwner(userID), id)
	if err != nil {
		return nil, err
	}
	from, to := budgetPeriodWindow(b.Period, on)
	spent, err := s.repo.CurrentPeriodSpend(ctx, userID, b.CategoryID, from, to)
	if err != nil {
		return nil, fmt.Errorf("compute spend: %w", err)
	}
	limits, err := s.limits(ctx, b)
	if err != nil {
		return nil, err
	}
	limit := limits.at(from)
	carry, err := s.carryoverWith(ctx, userID, b, limits, from)
	if err != nil {
		return nil, err
	}
	available := limit.Add(carry)
	remaining := available.Sub(spent)
	pct := 0.0
	if available.IsPositive() {
		p, _ := spent.Div(available).Float64()
		pct = p
	}
	return &BudgetSpend{
		BudgetID:    b.ID,
		CategoryID:  b.CategoryID,
		Period:      b.Period,
		PeriodStart: from,
		PeriodEnd:   to,
		Limit:       limit,
		Carryover:   carry,
		Available:   available,
		Spent:       spent,
		Remaining:   remaining,
		Pct:         pct,
	}, nil
}

// maxRolloverPeriods bounds how far back rollover looks, so one query stays
// cheap and a long-lived weekly budget doesn't accumulate unbounded history.
const maxRolloverPeriods = 36

// carryover returns the balance rolled into the period starting at
// currentFrom. Zero unless b.Rollover. Policy: the balance starts at
// b.RolloverStart (default zero) in the period containing the budget's
// creation; each completed period adds
// (limit - spent) and the running balance never goes below zero, so an
// overspent period does not penalize later ones. Each past period adds the
// limit in effect for it (#580).
func (s *BudgetService) carryover(ctx context.Context, userID int64, b *model.Budget, currentFrom time.Time) (decimal.Decimal, error) {
	if !b.Rollover {
		return decimal.Zero, nil
	}
	limits, err := s.limits(ctx, b)
	if err != nil {
		return decimal.Zero, err
	}
	return s.carryoverWith(ctx, userID, b, limits, currentFrom)
}

func (s *BudgetService) carryoverWith(ctx context.Context, userID int64, b *model.Budget, limits budgetLimits, currentFrom time.Time) (decimal.Decimal, error) {
	if !b.Rollover {
		return decimal.Zero, nil
	}
	start, _ := budgetPeriodWindow(b.Period, b.CreatedAt)
	// Nothing had rolled yet in a period before the budget existed (#577).
	if currentFrom.Before(start) {
		return decimal.Zero, nil
	}
	// Walk back at most maxRolloverPeriods from the current period.
	earliest := currentFrom
	for i := 0; i < maxRolloverPeriods && earliest.After(start); i++ {
		earliest = prevPeriodStart(b.Period, earliest)
	}
	// Past the lookback cap the starting balance has aged out with the
	// history it would have been folded into.
	carry := decimal.Zero
	if !earliest.After(start) {
		carry = b.RolloverStart
	}
	if !earliest.Before(currentFrom) {
		return carry, nil
	}
	unit := map[string]string{BudgetPeriodWeekly: "week", BudgetPeriodAnnual: "year"}[b.Period]
	if unit == "" {
		unit = "month"
	}
	spend, err := s.repo.SpendByPeriod(ctx, userID, b.CategoryID, unit, earliest, currentFrom)
	if err != nil {
		return decimal.Zero, fmt.Errorf("compute rollover: %w", err)
	}
	for p := earliest; p.Before(currentFrom); p = nextPeriodStart(b.Period, p) {
		carry = decimal.Max(decimal.Zero, carry.Add(limits.at(p)).Sub(spend[p]))
	}
	return carry, nil
}

// budgetLimits is a budget's limit history (#580), oldest first, plus its
// current amount.
type budgetLimits struct {
	history []model.BudgetAmount
	current decimal.Decimal
}

func (s *BudgetService) limits(ctx context.Context, b *model.Budget) (budgetLimits, error) {
	h, err := s.repo.AmountHistory(ctx, b.ID)
	if err != nil {
		return budgetLimits{}, fmt.Errorf("budget limit history: %w", err)
	}
	return budgetLimits{history: h, current: b.Amount}, nil
}

// at returns the limit for the period starting at periodStart: the latest
// entry effective on or before it. The latest entry is always the budget's
// current amount (budgets.amount is canonical during the expand stage); a
// period before the first entry uses the first entry, the oldest limit known.
func (l budgetLimits) at(periodStart time.Time) decimal.Decimal {
	n := len(l.history)
	if n == 0 {
		return l.current
	}
	day := time.Date(periodStart.Year(), periodStart.Month(), periodStart.Day(), 0, 0, 0, 0, time.UTC)
	i := n - 1
	for i >= 0 && l.history[i].EffectiveFrom.UTC().After(day) {
		i--
	}
	if i == n-1 {
		return l.current
	}
	if i < 0 {
		i = 0
	}
	return l.history[i].Amount
}

func prevPeriodStart(period string, from time.Time) time.Time {
	switch period {
	case BudgetPeriodWeekly:
		return from.AddDate(0, 0, -7)
	case BudgetPeriodAnnual:
		return from.AddDate(-1, 0, 0)
	default:
		return from.AddDate(0, -1, 0)
	}
}

func nextPeriodStart(period string, from time.Time) time.Time {
	switch period {
	case BudgetPeriodWeekly:
		return from.AddDate(0, 0, 7)
	case BudgetPeriodAnnual:
		return from.AddDate(1, 0, 0)
	default:
		return from.AddDate(0, 1, 0)
	}
}

// BudgetAlertSeverity classifies a budget over its threshold. "warning"
// applies in [0.8, 1.0); "over" applies at pct ≥ 1.0.
type BudgetAlertSeverity string

const (
	AlertWarning BudgetAlertSeverity = "warning"
	AlertOver    BudgetAlertSeverity = "over"
)

// BudgetAlert is one item in the dashboard alerts response. The frontend
// renders these as cards, color-coded by severity, linking back to the
// budget on /budgets.
type BudgetAlert struct {
	BudgetID     int64               `json:"budget_id"`
	CategoryID   int64               `json:"category_id"`
	CategoryName string              `json:"category_name"`
	Period       string              `json:"period"`
	Spent        decimal.Decimal     `json:"spent"`
	Limit        decimal.Decimal     `json:"limit"`
	Pct          float64             `json:"pct"`
	Severity     BudgetAlertSeverity `json:"severity"`
}

// Alerts returns all of the user's active budgets that are at 80%+ of their
// limit for the current period. Excludes soft-deleted and inactive budgets,
// and excludes anything under 80%.
//
// Implementation: one DB hit per distinct period (typically just monthly).
// We deliberately do NOT call Spend() per budget — that would be N
// round-trips for a hot dashboard endpoint.
func (s *BudgetService) Alerts(ctx context.Context, userID int64) ([]BudgetAlert, error) {
	budgets, err := s.repo.List(ctx, repository.UserOwner(userID))
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	// Active only — repo returns both for the budgets page, but alerts
	// shouldn't surface a paused budget.
	active := budgets[:0]
	for _, b := range budgets {
		if b.IsActive {
			active = append(active, b)
		}
	}
	if len(active) == 0 {
		return []BudgetAlert{}, nil
	}

	// Bucket budgets by period so we issue exactly one spend-by-category
	// query per distinct period. Most users only have one period, so this
	// is one query.
	byPeriod := map[string][]model.Budget{}
	for _, b := range active {
		byPeriod[b.Period] = append(byPeriod[b.Period], b)
	}

	// We also need category names for the response. One round-trip for the
	// distinct ids.
	catIDs := make([]int64, 0, len(active))
	seen := map[int64]struct{}{}
	for _, b := range active {
		if _, ok := seen[b.CategoryID]; ok {
			continue
		}
		seen[b.CategoryID] = struct{}{}
		catIDs = append(catIDs, b.CategoryID)
	}
	catNames := map[int64]string{}
	for _, id := range catIDs {
		c, err := s.catRepo.GetVisible(ctx, &userID, id)
		if err != nil {
			// Category was deleted out from under the budget — fall back
			// to "#<id>" so the alert still renders.
			catNames[id] = fmt.Sprintf("#%d", id)
			continue
		}
		catNames[id] = c.Name
	}

	now := s.now()
	out := make([]BudgetAlert, 0, len(active))
	for period, bucket := range byPeriod {
		from, to := budgetPeriodWindow(period, now)
		// Distinct categories in this bucket.
		bucketCatIDs := make([]int64, 0, len(bucket))
		seen := map[int64]struct{}{}
		for _, b := range bucket {
			if _, ok := seen[b.CategoryID]; ok {
				continue
			}
			seen[b.CategoryID] = struct{}{}
			bucketCatIDs = append(bucketCatIDs, b.CategoryID)
		}
		spend, err := s.repo.SpendByCategoryInRange(ctx, userID, bucketCatIDs, from, to)
		if err != nil {
			return nil, fmt.Errorf("spend by category: %w", err)
		}
		for _, b := range bucket {
			spent, ok := spend[b.CategoryID]
			if !ok {
				spent = decimal.Zero
			}
			available := b.Amount
			if b.Rollover {
				carry, err := s.carryover(ctx, userID, &b, from)
				if err != nil {
					return nil, err
				}
				available = available.Add(carry)
			}
			pct := 0.0
			if available.IsPositive() {
				p, _ := spent.Div(available).Float64()
				pct = p
			}
			if pct < 0.8 {
				continue
			}
			sev := AlertWarning
			if pct >= 1.0 {
				sev = AlertOver
			}
			out = append(out, BudgetAlert{
				BudgetID:     b.ID,
				CategoryID:   b.CategoryID,
				CategoryName: catNames[b.CategoryID],
				Period:       b.Period,
				Spent:        spent,
				Limit:        available,
				Pct:          pct,
				Severity:     sev,
			})
		}
	}
	return out, nil
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
