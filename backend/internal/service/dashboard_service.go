package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gregwym/offbook/backend/internal/repository"
)

// Supported period keys for ?period=
const (
	PeriodCurrentMonth = "current_month"
	PeriodLast30D      = "last_30d"
	PeriodYTD          = "ytd"
)

// ErrInvalidPeriod is returned when the request asks for a period we don't
// implement. New periods belong in resolvePeriod, not in handler switch
// statements.
var ErrInvalidPeriod = errors.New("invalid period")

// DashboardSummary mirrors the API response shape exactly. The handler can
// json-encode this directly.
type DashboardSummary struct {
	Period           PeriodWindow                    `json:"period"`
	NetWorth         string                          `json:"net_worth"`
	Income           string                          `json:"income"`
	Spending         string                          `json:"spending"`
	AccountCount     int64                           `json:"account_count"`
	TransactionCount int64                           `json:"transaction_count"`
	ByCategory       []DashboardCategorySpendingItem `json:"by_category"`
}

// PeriodWindow uses "from" inclusive, "to" exclusive (documented in the
// handler). Both are TIMESTAMPTZ-friendly RFC3339.
type PeriodWindow struct {
	Key  string    `json:"key"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// DashboardCategorySpendingItem is one row of the by-category breakdown.
type DashboardCategorySpendingItem struct {
	CategoryID *int64 `json:"category_id"`
	Name       string `json:"name"`
	Amount     string `json:"amount"`
}

// DashboardService composes the summary from the dashboard repo.
type DashboardService struct {
	repo repository.DashboardRepository
	now  func() time.Time // injected so tests can fix the clock
}

func NewDashboardService(repo repository.DashboardRepository) *DashboardService {
	return &DashboardService{repo: repo, now: time.Now}
}

// SetClock overrides the time source. Tests use this; production callers
// should not.
func (s *DashboardService) SetClock(fn func() time.Time) {
	s.now = fn
}

func (s *DashboardService) Summarize(ctx context.Context, userID int64, period string) (*DashboardSummary, error) {
	from, to, err := resolvePeriod(period, s.now())
	if err != nil {
		return nil, err
	}
	agg, err := s.repo.Summarize(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}

	items := make([]DashboardCategorySpendingItem, 0, len(agg.ByCategory))
	for _, row := range agg.ByCategory {
		items = append(items, DashboardCategorySpendingItem{
			CategoryID: row.CategoryID,
			Name:       row.Name,
			Amount:     row.Amount.String(),
		})
	}

	return &DashboardSummary{
		Period:           PeriodWindow{Key: period, From: from, To: to},
		NetWorth:         agg.NetWorth.String(),
		Income:           agg.Income.String(),
		Spending:         agg.Spending.String(),
		AccountCount:     agg.AccountCount,
		TransactionCount: agg.TransactionCount,
		ByCategory:       items,
	}, nil
}

// resolvePeriod returns the [from, to) window for the named period.
// All boundaries are computed in the local time zone of `now` — the dashboard
// is a user-facing view, not an audit log, so DST handling tracks the user's
// expectation that "this month" ends at local midnight.
func resolvePeriod(key string, now time.Time) (time.Time, time.Time, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = PeriodCurrentMonth
	}
	switch key {
	case PeriodCurrentMonth:
		from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		to := from.AddDate(0, 1, 0)
		return from, to, nil
	case PeriodLast30D:
		// Window the *day* — start at midnight 30 days ago, end at midnight tomorrow.
		// This keeps "last 30 days" stable regardless of when in the day the user loads.
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		from := today.AddDate(0, 0, -30)
		to := today.AddDate(0, 0, 1)
		return from, to, nil
	case PeriodYTD:
		from := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, now.Location())
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		to := today.AddDate(0, 0, 1)
		return from, to, nil
	default:
		return time.Time{}, time.Time{}, ErrInvalidPeriod
	}
}
