package household_test

import (
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/service/household"
)

// TestRecentInCurrentMonth_StaysInsideCurrentMonth pins the #693 fix: the
// seed instant the aggregator tests use must always resolve into
// PeriodCurrentMonth, including the first hour of the 1st, and must never be
// in the future.
func TestRecentInCurrentMonth_StaysInsideCurrentMonth(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"mid-month is now minus an hour", time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC), time.Date(2026, 5, 15, 11, 0, 0, 0, time.UTC)},
		{"first hour of the 1st clamps to month start", time.Date(2026, 11, 1, 0, 15, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{"exactly 01:00 on the 1st is month start", time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{"midnight on the 1st is month start", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recentInCurrentMonth(tc.now)
			if !got.Equal(tc.want) {
				t.Fatalf("recentInCurrentMonth(%v) = %v, want %v", tc.now, got, tc.want)
			}
			if got.After(tc.now) {
				t.Fatalf("seed %v is after now %v", got, tc.now)
			}
			from, to, err := household.ResolvePeriod(household.PeriodCurrentMonth, tc.now)
			if err != nil {
				t.Fatalf("ResolvePeriod: %v", err)
			}
			if got.Before(from) || !got.Before(to) {
				t.Fatalf("seed %v outside current month [%v, %v)", got, from, to)
			}
		})
	}
}
