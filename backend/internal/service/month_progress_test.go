package service_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/service"
)

func TestMonthProgress(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 12, 0, 0, 0, time.UTC) }
	tests := []struct {
		name                 string
		through, now         time.Time
		partial              bool
		elapsed, daysInMonth int
	}{
		{"current month day 3", d(2026, 11, 1), d(2026, 11, 3), true, 3, 30},
		{"leap February", d(2028, 2, 10), d(2028, 2, 29), true, 29, 29},
		{"stepped back", d(2026, 10, 1), d(2026, 11, 3), false, 0, 0},
		{"same month other year", d(2025, 11, 1), d(2026, 11, 3), false, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, e, n := service.MonthProgress(tc.through, tc.now)
			if p != tc.partial || e != tc.elapsed || n != tc.daysInMonth {
				t.Errorf("got %v %d/%d, want %v %d/%d", p, e, n, tc.partial, tc.elapsed, tc.daysInMonth)
			}
		})
	}
}

func TestProrateToDate(t *testing.T) {
	tests := []struct {
		name          string
		amount        string
		elapsed, days int
		want          string
	}{
		{"day 3 of 30", "300", 3, 30, "30"},
		{"exact decimals", "100.10", 15, 30, "50.05"},
		{"complete month", "300", 0, 0, "300"},
		{"last day", "300", 30, 30, "300"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := service.ProrateToDate(decimal.RequireFromString(tc.amount), tc.elapsed, tc.days)
			if !got.Equal(decimal.RequireFromString(tc.want)) {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
