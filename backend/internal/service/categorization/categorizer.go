package categorization

import (
	"context"
	"strings"
)

// CategoryOption is one candidate category a Categorizer may choose from.
type CategoryOption struct {
	ID   int64
	Slug string
}

// Candidate is one transaction offered to a Categorizer batch call. Fields
// are deliberately minimal (ADR-0022 §4): no account ID, no date, no raw
// Description, no user identifier — just enough merchant text to guess a
// category, plus the amount's sign (never the raw value).
type Candidate struct {
	// Ref is an opaque per-call index the categorizer must echo back in
	// Verdict.Ref so the caller can rejoin verdicts to rows. It carries no
	// meaning outside one Categorize call — it is NOT a transaction ID.
	Ref              int
	MerchantName     *string
	DescriptionClean *string
	// AmountSign is "debit" (money out) or "credit" (money in).
	AmountSign string
}

const (
	AmountSignDebit  = "debit"
	AmountSignCredit = "credit"
)

// Verdict is one categorization decision for a Candidate, matched back by
// Ref. Confidence is the model's self-reported certainty, 0..1.
type Verdict struct {
	Ref          int
	CategorySlug string
	Confidence   float64
}

// Categorizer is the AI transaction-categorization capability (ADR-0022),
// implemented per-provider in internal/service/ai (kept here, not in ai, so
// ai — which already imports the top-level service package — can implement
// this interface without an import cycle back from service).
type Categorizer interface {
	// Categorize proposes a category for each candidate it can, choosing
	// only from options. Implementations MUST NOT emit a CategorySlug
	// absent from options — callers treat an unknown slug as no verdict.
	// A Candidate with no confident guess may be omitted from the result
	// entirely; callers must not assume len(result) == len(candidates).
	Categorize(ctx context.Context, candidates []Candidate, options []CategoryOption) ([]Verdict, error)
	// Name is a short stable identifier ("claude") persisted as the verdict
	// cache's provenance (ai_categorization_verdicts.provider).
	Name() string
}

// NormalizeMerchantKey returns the cache key used for the AI verdict cache
// (ai_categorization_verdicts.merchant_key) and the "promote to rule"
// pattern seed: MerchantName if present, else DescriptionClean — uppercased
// and trimmed. Empty when neither field carries text (nothing to key on, so
// the row is skipped by the categorization pass).
func NormalizeMerchantKey(merchantName, descriptionClean *string) string {
	for _, p := range []*string{merchantName, descriptionClean} {
		if p == nil {
			continue
		}
		v := strings.ToUpper(strings.TrimSpace(*p))
		if v != "" {
			return v
		}
	}
	return ""
}
