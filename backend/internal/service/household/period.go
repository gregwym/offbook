package household

import (
	"errors"
	"strings"
	"time"
)

// Period keys understood by aggregator endpoints under /h/...
// Mirrors service.Period* constants intentionally — keeping a local copy
// means service/household has no import dependency on service/.
const (
	PeriodCurrentMonth = "current_month"
	PeriodLast30D      = "last_30d"
	PeriodYTD          = "ytd"
)

// ErrInvalidPeriod is returned for unknown period keys.
var ErrInvalidPeriod = errors.New("invalid period")

// ResolvePeriod returns the [from, to) window for the named period.
// Boundaries are local-time to match the personal dashboard's behavior.
func ResolvePeriod(key string, now time.Time) (time.Time, time.Time, error) {
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
