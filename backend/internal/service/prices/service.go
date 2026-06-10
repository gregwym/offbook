package prices

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

// RefreshResult is the wire response of a manual refresh. Skipped lists the
// held symbols no provider could quote — surfaced, not silent (#282 spirit):
// those assets will keep reporting stale/unpriced in valuations.
type RefreshResult struct {
	Refreshed int       `json:"refreshed"`
	Skipped   []string  `json:"skipped"`
	AsOf      time.Time `json:"as_of"`
}

// Service orchestrates providers: it derives the asset set from the user's
// current holdings, partitions it across registered providers, and appends
// the returned observations to the prices time series. It never reads or
// writes positions/transactions beyond listing held assets.
type Service struct {
	users     repository.UserRepository
	positions repository.PositionRepository
	assets    repository.AssetRepository
	prices    repository.PriceRepository
	providers []Provider
	now       func() time.Time
}

func NewService(
	users repository.UserRepository,
	positions repository.PositionRepository,
	assets repository.AssetRepository,
	prices repository.PriceRepository,
	providers ...Provider,
) *Service {
	return &Service{
		users:     users,
		positions: positions,
		assets:    assets,
		prices:    prices,
		providers: providers,
		now:       time.Now,
	}
}

// SetClock overrides the time source (tests).
func (s *Service) SetClock(fn func() time.Time) { s.now = fn }

// RefreshForUser fetches current prices for every asset the user holds
// (positions ≠ 0), quoted in the user's primary currency, and appends the
// observations to `prices`. The egress is the symbol list only, and only
// for this user's holdings — never another tenant's (the position read is
// user-scoped). Held assets no provider can quote are returned in Skipped.
func (s *Service) RefreshForUser(ctx context.Context, userID int64) (*RefreshResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("prices: load user: %w", err)
	}

	allAssets, err := s.assets.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("prices: list assets: %w", err)
	}
	assetByID := make(map[int64]model.Asset, len(allAssets))
	for _, a := range allAssets {
		assetByID[a.ID] = a
	}
	quote, ok := assetByID[user.PrimaryCurrencyAssetID]
	if !ok {
		return nil, fmt.Errorf("prices: primary currency asset %d not found", user.PrimaryCurrencyAssetID)
	}

	positions, err := s.positions.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("prices: list positions: %w", err)
	}
	seen := map[int64]bool{}
	held := make([]model.Asset, 0, len(positions))
	for _, p := range positions {
		if p.Quantity.IsZero() || p.AssetID == quote.ID || seen[p.AssetID] {
			continue
		}
		seen[p.AssetID] = true
		a, ok := assetByID[p.AssetID]
		if !ok {
			continue
		}
		held = append(held, a)
	}

	result := &RefreshResult{Skipped: []string{}, AsOf: s.now().UTC()}
	remaining := held
	for _, provider := range s.providers {
		supported := make([]model.Asset, 0, len(remaining))
		next := make([]model.Asset, 0, len(remaining))
		for _, a := range remaining {
			if provider.Supports(a) {
				supported = append(supported, a)
			} else {
				next = append(next, a)
			}
		}
		remaining = next
		if len(supported) == 0 {
			continue
		}
		quotes, err := provider.Fetch(ctx, supported, quote)
		if err != nil {
			return nil, fmt.Errorf("prices: provider %s: %w", provider.Name(), err)
		}
		quoted := map[int64]bool{}
		for _, q := range quotes {
			if err := s.prices.Insert(ctx, &model.Price{
				AssetID:      q.AssetID,
				QuoteAssetID: q.QuoteAssetID,
				AsOf:         q.AsOf,
				Price:        q.Price,
				Source:       provider.Name(),
			}); err != nil {
				return nil, fmt.Errorf("prices: insert %s observation: %w", provider.Name(), err)
			}
			quoted[q.AssetID] = true
			result.Refreshed++
		}
		// Supported but not returned (unknown to upstream, unquotable
		// currency) → back into the pool so a later provider may cover it,
		// or it lands in Skipped.
		for _, a := range supported {
			if !quoted[a.ID] {
				remaining = append(remaining, a)
			}
		}
	}

	for _, a := range remaining {
		result.Skipped = append(result.Skipped, strings.ToUpper(a.Symbol))
	}
	sort.Strings(result.Skipped)
	return result, nil
}
