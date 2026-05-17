// Package testutil holds shared test helpers. The PII leak scanner is the
// enforcement arm of .claude/rules/testing.md ("PII tests: verify pii_store
// is the only table containing PII after any operation"). See ADR-0003
// for the architectural intent.
package testutil

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// PIIScanTarget pairs a table with the text/JSONB columns on it that could
// plausibly leak PII. JSONB columns are cast to text in the query so the
// same ILIKE works uniformly.
type PIIScanTarget struct {
	Table   string
	Columns []string
}

// DefaultPIIScanTargets enumerates the non-pii_store tables and columns
// that could leak PII today. Add to this list whenever a new free-text
// column lands on a domain table.
func DefaultPIIScanTargets() []PIIScanTarget {
	return []PIIScanTarget{
		{Table: "accounts", Columns: []string{"name"}},
		{Table: "transactions", Columns: []string{"description", "description_clean", "merchant_name", "notes"}},
		{Table: "ai_messages", Columns: []string{"content", "context_snapshot"}},
		{Table: "categories", Columns: []string{"name"}},
	}
}

// PIILeak describes one match. Reported verbatim so failing tests point
// directly at the offending row.
type PIILeak struct {
	Table  string
	Column string
	RowID  int64
	Token  string
}

func (l PIILeak) String() string {
	return fmt.Sprintf("%s.%s row id=%d matched %q", l.Table, l.Column, l.RowID, l.Token)
}

// ScanForPIILeaks runs a case-insensitive substring search across the
// configured targets for each token. Returns every match (caller decides
// whether to fail loud or summarize).
//
// Tokens are trimmed; empty tokens are skipped to avoid matching every row.
// JSONB columns are cast via `::text` so the same ILIKE pattern works.
func ScanForPIILeaks(ctx context.Context, db *gorm.DB, tokens []string, targets []PIIScanTarget) ([]PIILeak, error) {
	cleanedTokens := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		cleanedTokens = append(cleanedTokens, t)
	}
	if len(cleanedTokens) == 0 {
		return nil, nil
	}

	var leaks []PIILeak
	for _, target := range targets {
		for _, col := range target.Columns {
			for _, token := range cleanedTokens {
				// `column::text` is a no-op for text/varchar and the
				// correct cast for JSONB / other types. Quoting the
				// identifiers protects against reserved words.
				query := fmt.Sprintf(`SELECT id FROM %q WHERE %q::text ILIKE ?`, target.Table, col)
				rows, err := db.WithContext(ctx).Raw(query, "%"+token+"%").Rows()
				if err != nil {
					return nil, fmt.Errorf("scan %s.%s for %q: %w", target.Table, col, token, err)
				}
				for rows.Next() {
					var id int64
					if scanErr := rows.Scan(&id); scanErr != nil {
						_ = rows.Close()
						return nil, fmt.Errorf("scan id from %s.%s: %w", target.Table, col, scanErr)
					}
					leaks = append(leaks, PIILeak{Table: target.Table, Column: col, RowID: id, Token: token})
				}
				if err := rows.Close(); err != nil {
					return nil, fmt.Errorf("close rows for %s.%s: %w", target.Table, col, err)
				}
			}
		}
	}

	// Stable order for deterministic failure messages.
	sort.Slice(leaks, func(i, j int) bool {
		if leaks[i].Table != leaks[j].Table {
			return leaks[i].Table < leaks[j].Table
		}
		if leaks[i].Column != leaks[j].Column {
			return leaks[i].Column < leaks[j].Column
		}
		if leaks[i].RowID != leaks[j].RowID {
			return leaks[i].RowID < leaks[j].RowID
		}
		return leaks[i].Token < leaks[j].Token
	})
	return leaks, nil
}

// AssertNoPIILeak fails the test if any token from `tokens` appears in any
// non-pii_store column listed by DefaultPIIScanTargets. Each match is
// reported on its own line so an audit-style test surfaces every offender
// in a single run, not one at a time.
func AssertNoPIILeak(t *testing.T, db *gorm.DB, tokens []string) {
	t.Helper()
	leaks, err := ScanForPIILeaks(t.Context(), db, tokens, DefaultPIIScanTargets())
	if err != nil {
		t.Fatalf("pii scan failed: %v", err)
	}
	if len(leaks) == 0 {
		return
	}
	lines := make([]string, 0, len(leaks)+1)
	lines = append(lines, fmt.Sprintf("found %d PII leak(s) in non-pii_store tables:", len(leaks)))
	for _, l := range leaks {
		lines = append(lines, "  "+l.String())
	}
	t.Fatal(strings.Join(lines, "\n"))
}
