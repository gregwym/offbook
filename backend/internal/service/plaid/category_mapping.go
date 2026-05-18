package plaid

import (
	"context"

	"github.com/gregwym/offbook/backend/internal/repository"
)

// CategoryMapper resolves a Plaid PFC (primary, detailed) pair to a local
// categories.id. Loaded from plaid_category_map once at service construction;
// callers MUST treat the resolved id as a default that user choice overrides.
//
// Lookup is O(1) on a composite string key. A nil receiver is treated as
// "no mapping configured" so the service can be wired in degraded mode
// without panicking (e.g., when DB is unreachable at startup).
type CategoryMapper struct {
	// table is keyed by primary + "|" + detailed. The "|" separator is
	// safe because Plaid PFC tokens are ALL_CAPS_WITH_UNDERSCORES and
	// never contain pipe characters.
	table map[string]int64
}

// NewCategoryMapper loads all rows from the repo and builds the in-memory
// lookup. Returns an empty (but non-nil) mapper if the table is empty so
// callers don't need a separate nil check.
func NewCategoryMapper(ctx context.Context, repo repository.PlaidCategoryMapRepository) (*CategoryMapper, error) {
	rows, err := repo.LoadAll(ctx)
	if err != nil {
		return nil, err
	}
	m := &CategoryMapper{table: make(map[string]int64, len(rows))}
	for _, r := range rows {
		m.table[pfcKey(r.PlaidPrimary, r.PlaidDetailed)] = r.CategoryID
	}
	return m, nil
}

// MapPlaidCategory returns the local category_id for a Plaid PFC pair.
// ok=false when the pair isn't mapped — caller should leave category_id NULL.
func (m *CategoryMapper) MapPlaidCategory(primary, detailed string) (int64, bool) {
	if m == nil || primary == "" || detailed == "" {
		return 0, false
	}
	id, ok := m.table[pfcKey(primary, detailed)]
	return id, ok
}

// Size reports how many mappings are loaded. Useful for /health-style
// surfaces and for tests that want to assert the seed actually loaded.
func (m *CategoryMapper) Size() int {
	if m == nil {
		return 0
	}
	return len(m.table)
}

func pfcKey(primary, detailed string) string {
	return primary + "|" + detailed
}

// CategorizationMethodPlaidDefault is the value written into
// transactions.categorization_method when the Plaid PFC mapper assigned
// a default category. The other documented value is "manual" (user
// picked the category). Both are documented in docs/ARCHITECTURE.md.
const CategorizationMethodPlaidDefault = "plaid_default"
