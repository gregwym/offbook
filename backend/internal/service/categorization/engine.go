// Package categorization implements the user-rule-driven category assignment
// engine for transactions. It is deliberately pure-Go: it takes a slice of
// precompiled rules + the relevant transaction text fields and returns the
// first matching rule's decision. Rule loading, ordering, and storage live
// elsewhere (service.CategorizationRuleService + the repo). This split lets
// callers load rules once per request (e.g. once per Plaid sync) and reuse
// the compiled slice across hundreds of transactions without re-paying the
// regex-compile cost.
package categorization

import (
	"regexp"
	"strings"

	"github.com/gregwym/offbook/backend/internal/model"
)

// MethodRule is the value written into transactions.categorization_method
// when a user rule matched. The other documented values are "manual" (user
// picked) and "plaid_default" (Plaid PFC mapper).
const MethodRule = "rule"

// CompiledRule pairs a stored rule with its precompiled regex (when the
// match_type is "regex"). Re is nil for contains/exact rules. AssetID,
// when non-nil, narrows the rule to transactions whose asset_id matches
// — see Categorize.
type CompiledRule struct {
	ID         int64
	CategoryID int64
	MatchType  string
	Pattern    string
	AssetID    *int64
	patternCI  string // pattern uppercased once, for contains/exact fast path
	Re         *regexp.Regexp
}

// Decision is what Categorize returns when a rule matched.
type Decision struct {
	RuleID     int64
	CategoryID int64
}

// Compile turns stored rules into matcher-ready CompiledRule values. The
// input slice should already be ordered priority DESC, id ASC by the
// repository — Compile preserves order. Inactive rules and rules with an
// invalid regex pattern are skipped silently: the service-layer create
// path already rejects invalid regexes, so reaching this branch means a
// rule was corrupted out-of-band; either way the engine refuses to crash.
func Compile(rules []model.CategorizationRule) []CompiledRule {
	out := make([]CompiledRule, 0, len(rules))
	for _, r := range rules {
		if !r.IsActive {
			continue
		}
		c := CompiledRule{
			ID:         r.ID,
			CategoryID: r.CategoryID,
			MatchType:  r.MatchType,
			Pattern:    r.Pattern,
			AssetID:    r.AssetID,
			patternCI:  strings.ToUpper(r.Pattern),
		}
		if r.MatchType == "regex" {
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				continue
			}
			c.Re = re
		}
		out = append(out, c)
	}
	return out
}

// Categorize evaluates rules in order and returns the first match. The
// matcher considers, in order: description_clean, description, merchant_name.
// "contains" and "exact" are case-insensitive; "regex" uses the pattern as
// authored (callers can prefix `(?i)` if they want case-insensitivity).
//
// assetID, when non-zero, narrows the candidate set: rules with a non-nil
// CompiledRule.AssetID only fire on a matching assetID. Rules with a nil
// AssetID stay asset-agnostic. Pass 0 to skip the asset filter entirely
// (matches the M2-era behavior for callers that pre-date the column).
func Categorize(rules []CompiledRule, assetID int64, descriptionClean, description, merchantName *string) (Decision, bool) {
	if len(rules) == 0 {
		return Decision{}, false
	}
	fields := collectFields(descriptionClean, description, merchantName)
	for _, r := range rules {
		if r.AssetID != nil {
			if assetID == 0 || *r.AssetID != assetID {
				continue
			}
		}
		// Asset-bound rules with no pattern match purely on asset_id —
		// useful for "all AAPL legs → Investments". collectFields can
		// return empty for trade legs (which have no merchant text);
		// don't reject them up-front.
		if len(fields) == 0 {
			if r.AssetID != nil && r.Pattern == "" {
				return Decision{RuleID: r.ID, CategoryID: r.CategoryID}, true
			}
			continue
		}
		if matches(r, fields) {
			return Decision{RuleID: r.ID, CategoryID: r.CategoryID}, true
		}
	}
	return Decision{}, false
}

// Apply runs Categorize and, when a rule matches, mutates tx in place to
// reflect the decision: sets CategoryID, CategorizationMethod="rule", and
// CategorizationRuleID. Returns the decision and whether a rule matched.
// Caller is responsible for deciding whether to call Apply — typically only
// when the row has no existing category.
func Apply(tx *model.Transaction, rules []CompiledRule) (Decision, bool) {
	if tx == nil {
		return Decision{}, false
	}
	d, ok := Categorize(rules, tx.AssetID, tx.DescriptionClean, tx.Description, tx.MerchantName)
	if !ok {
		return Decision{}, false
	}
	catID := d.CategoryID
	ruleID := d.RuleID
	method := MethodRule
	tx.CategoryID = &catID
	tx.CategorizationRuleID = &ruleID
	tx.CategorizationMethod = &method
	return d, true
}

// collectFields returns the non-nil, non-empty text fields the matcher
// inspects, in priority order. We pass both the raw and uppercased form
// because contains/exact want CI compare while regex wants the raw value.
type field struct {
	raw   string
	upper string
}

func collectFields(descriptionClean, description, merchantName *string) []field {
	var out []field
	add := func(p *string) {
		if p == nil {
			return
		}
		v := strings.TrimSpace(*p)
		if v == "" {
			return
		}
		out = append(out, field{raw: v, upper: strings.ToUpper(v)})
	}
	add(descriptionClean)
	add(description)
	add(merchantName)
	return out
}

func matches(r CompiledRule, fields []field) bool {
	switch r.MatchType {
	case "contains":
		for _, f := range fields {
			if strings.Contains(f.upper, r.patternCI) {
				return true
			}
		}
	case "exact":
		for _, f := range fields {
			if f.upper == r.patternCI {
				return true
			}
		}
	case "regex":
		if r.Re == nil {
			return false
		}
		for _, f := range fields {
			if r.Re.MatchString(f.raw) {
				return true
			}
		}
	}
	return false
}
