package service

import (
	"time"

	"github.com/shopspring/decimal"
)

// MonthProgress reports whether the calendar month containing through
// (UTC) is still in progress at now, and if so how many of its days have
// elapsed, today included, and how many days it has (#695). A completed
// month, past or stepped back to, returns partial=false.
func MonthProgress(through, now time.Time) (partial bool, elapsed, days int) {
	t, n := through.UTC(), now.UTC()
	if t.Year() != n.Year() || t.Month() != n.Month() {
		return false, 0, 0
	}
	first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC)
	days = first.AddDate(0, 1, -1).Day()
	return true, n.Day(), days
}

// ProrateToDate scales a full-month amount to the first `elapsed` of `days`
// days (#695): the typical spend by this date. Exact decimal arithmetic;
// with no elapsed/days information it returns the amount unchanged.
func ProrateToDate(amount decimal.Decimal, elapsed, days int) decimal.Decimal {
	if days <= 0 || elapsed >= days {
		return amount
	}
	return amount.Mul(decimal.NewFromInt(int64(elapsed))).Div(decimal.NewFromInt(int64(days)))
}
