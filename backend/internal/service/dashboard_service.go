package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/valuation"
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

// SpendByCategoryItem is one row of the spend-by-category chart payload.
// Amount is a positive decimal string (outflow sign flipped).
type SpendByCategoryItem struct {
	CategoryID *int64 `json:"category_id"`
	Name       string `json:"name"`
	Color      string `json:"color,omitempty"`
	Amount     string `json:"amount"`
}

// CashFlowMonth mirrors repository.CashFlowMonth for the API. Month is
// the first-of-month timestamp (UTC), formatted as YYYY-MM-DD on the
// wire.
type CashFlowMonth struct {
	Month   string `json:"month"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

// NetWorthPoint is one month-end of the net-worth trend. Complete is false
// when at least one held asset had no available price at that month-end, so
// Total is a partial sum (#282) — the UI can flag "incomplete" rather than
// present a confident-but-wrong figure.
type NetWorthPoint struct {
	Date     string `json:"date"`
	Total    string `json:"total"`
	Complete bool   `json:"complete"`
}

// DashboardService composes the summary from the dashboard repo.
type DashboardService struct {
	repo  repository.DashboardRepository
	txns  repository.TransactionRepository
	users repository.UserRepository
	val   *valuation.Service
	now   func() time.Time // injected so tests can fix the clock
}

func NewDashboardService(
	repo repository.DashboardRepository,
	txns repository.TransactionRepository,
	users repository.UserRepository,
	val *valuation.Service,
) *DashboardService {
	return &DashboardService{repo: repo, txns: txns, users: users, val: val, now: time.Now}
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

// SpendByCategory returns the pie-chart payload for [from, to). Defaults
// to the current month if either bound is zero.
func (s *DashboardService) SpendByCategory(ctx context.Context, userID int64, from, to time.Time) ([]SpendByCategoryItem, error) {
	if from.IsZero() || to.IsZero() {
		f, t, _ := resolvePeriod(PeriodCurrentMonth, s.now())
		from, to = f, t
	}
	rows, err := s.repo.SpendByCategory(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]SpendByCategoryItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, SpendByCategoryItem{
			CategoryID: r.CategoryID,
			Name:       r.Name,
			Color:      r.Color,
			Amount:     r.Amount.String(),
		})
	}
	return out, nil
}

// CashFlow returns the trailing `months` months of in/out/net. Defaults
// to 12.
func (s *DashboardService) CashFlow(ctx context.Context, userID int64, months int) ([]CashFlowMonth, error) {
	if months <= 0 {
		months = 12
	}
	rows, err := s.repo.CashFlowByMonth(ctx, userID, s.now(), months)
	if err != nil {
		return nil, err
	}
	out := make([]CashFlowMonth, 0, len(rows))
	for _, r := range rows {
		out = append(out, CashFlowMonth{
			Month:   r.Month.Format("2006-01-02"),
			Inflow:  r.Inflow.String(),
			Outflow: r.Outflow.String(),
			Net:     r.Net.String(),
		})
	}
	return out, nil
}

// NetWorth returns the trailing `months` months of month-end net worth,
// computed by the unified valuation derivation (#282): at each month-end the
// quantity held per asset is the transaction fold up to that date, priced at
// that date's rate in the user's primary currency. This replaces the old
// back-derivation, which subtracted raw transaction amounts across differing
// assets (shares vs dollars) from a constant present-day total. Defaults to 12.
func (s *DashboardService) NetWorth(ctx context.Context, userID int64, months int) ([]NetWorthPoint, error) {
	if months <= 0 {
		months = 12
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	asOfs := monthEndGrid(s.now().UTC(), months)
	// Any-age prices are acceptable for a historical trend; the freshness
	// gate exists to flag a stale *current* price, not to blank out history.
	series, err := s.val.WithStaleWindow(0).Series(ctx, asOfs, user.PrimaryCurrencyAssetID,
		func(ctx context.Context, asOf time.Time) ([]model.Position, error) {
			return s.txns.FoldByUserAsOf(ctx, userID, asOf)
		})
	if err != nil {
		return nil, err
	}

	out := make([]NetWorthPoint, 0, len(series))
	for _, p := range series {
		out = append(out, NetWorthPoint{
			Date:     p.AsOf.Format("2006-01-02"),
			Total:    p.Value.String(),
			Complete: p.Complete(),
		})
	}
	return out, nil
}

// monthEndGrid returns the last-instant-of-month for the trailing `months`
// months ending in now's month, oldest first. Each boundary is 23:59:59.999…
// UTC of the month's last day so a price/transaction dated anywhere that day
// is included.
func monthEndGrid(now time.Time, months int) []time.Time {
	startMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -months+1, 0)
	out := make([]time.Time, 0, months)
	for i := 0; i < months; i++ {
		firstOfNext := startMonth.AddDate(0, i+1, 0)
		monthEnd := firstOfNext.Add(-time.Nanosecond)
		out = append(out, monthEnd)
	}
	return out
}

// TradeSummary is the AI-context view of a user's trade activity over
// a window. Per-asset detail intentionally rolls up to asset kind so
// the LLM sees the shape of the portfolio's activity without per-row
// quantities or tickers — keeps the prompt budget small and shields
// fingerprintable precision from the provider.
type TradeSummary struct {
	From      time.Time           `json:"from"`
	To        time.Time           `json:"to"`
	TotalLegs int64               `json:"total_legs"`
	ByKind    []TradeKindSnapshot `json:"by_kind"`
}

// TradeKindSnapshot is one row of TradeSummary.
type TradeKindSnapshot struct {
	Kind       string `json:"kind"`
	LegCount   int64  `json:"leg_count"`
	GrossValue string `json:"gross_value"`
}

// Trades returns the trade-activity rollup over [from, to). Used by
// the AI context builder; produces aggregates only — no raw rows, no
// tickers, no per-trade quantities.
func (s *DashboardService) Trades(ctx context.Context, userID int64, from, to time.Time) (*TradeSummary, error) {
	if from.IsZero() {
		from = s.now().AddDate(0, -3, 0)
	}
	if to.IsZero() {
		to = s.now().AddDate(0, 0, 1)
	}
	rows, err := s.repo.TradeSummaryByKind(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	out := &TradeSummary{From: from, To: to}
	for _, r := range rows {
		out.TotalLegs += r.LegCount
		out.ByKind = append(out.ByKind, TradeKindSnapshot{
			Kind:       r.Kind,
			LegCount:   r.LegCount,
			GrossValue: r.GrossValue.String(),
		})
	}
	return out, nil
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
