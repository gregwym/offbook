package valuation_test

import (
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/service/valuation"
)

// TestTrimUnknownStart (#618): leading month-ends with no position are unknown
// history and dropped; once the set holds something, later empty months are
// known zeros and stay.
func TestTrimUnknownStart(t *testing.T) {
	grid := valuation.MonthEndGrid(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), 4) // Feb … May
	series := make([]valuation.SeriesPoint, len(grid))
	for i, at := range grid {
		series[i] = valuation.SeriesPoint{AsOf: at}
	}
	tests := []struct {
		name string
		held []bool
		want []time.Time
	}{
		{"never held", []bool{false, false, false, false}, nil},
		{"held from the start", []bool{true, true, true, true}, grid},
		{"starts in April", []bool{false, false, true, true}, grid[2:]},
		{"drawn to zero later stays", []bool{false, true, false, true}, grid[1:]},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			held := map[time.Time]bool{}
			for i, h := range tc.held {
				held[grid[i]] = h
			}
			got := valuation.TrimUnknownStart(series, held)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d points, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if !got[i].AsOf.Equal(tc.want[i]) {
					t.Errorf("point[%d] = %s, want %s", i, got[i].AsOf, tc.want[i])
				}
			}
		})
	}
}
