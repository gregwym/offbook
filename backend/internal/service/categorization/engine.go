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
	"time"

	"github.com/shopspring/decimal"

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
	ID int64
	// CategoryID is nil for a rule that keeps the row's category (#553).
	CategoryID *int64
	MatchType  string
	Pattern    string
	AssetID    *int64
	// RenameMerchant, when non-nil, replaces merchant_name on rows this
	// rule wins (#534).
	RenameMerchant *string
	// Hide, when true, hides the flow rows this rule wins (#536).
	Hide bool
	// MarkReviewed, when true, marks the flow rows this rule wins reviewed (#538).
	MarkReviewed bool
	// AddTags are added to the flow rows this rule wins (#546).
	AddTags []string
	// AccountID, AmountMin and AmountMax narrow the rule to one account
	// and to a band of |amount|, inclusive (#550). Nil = unconstrained.
	AccountID *int64
	AmountMin *decimal.Decimal
	AmountMax *decimal.Decimal
	patternCI string // pattern uppercased once, for contains/exact fast path
	Re        *regexp.Regexp
}

// Decision is what Categorize returns when a rule matched.
type Decision struct {
	RuleID int64
	// CategoryID is nil when the winning rule keeps the row's category and
	// only takes its other actions (#553). Such a rule still wins: later
	// rules don't run on the row.
	CategoryID *int64
	// RenameMerchant is the winning rule's merchant rename, nil when it
	// has none.
	RenameMerchant *string
	// Hide is true when the winning rule hides its matches (#536).
	Hide bool
	// MarkReviewed is true when the winning rule marks its matches
	// reviewed (#538).
	MarkReviewed bool
	// AddTags are the winning rule's tags to add (#546).
	AddTags []string
}

// MerchantChanged reports whether applying d would change t's merchant. A
// merchant the user fixed by hand is pinned (#569): rules never rename it.
func (d Decision) MerchantChanged(t *model.Transaction) bool {
	return d.RenameMerchant != nil && t.MerchantEditedAt == nil &&
		(t.MerchantName == nil || *t.MerchantName != *d.RenameMerchant)
}

// SetsCategory reports whether the winning rule sets a category. A rule
// without one never writes category_id, categorization_method or
// categorization_rule_id (#553).
func (d Decision) SetsCategory() bool { return d.CategoryID != nil }

// CategoryChanged reports whether applying d would change t's category.
// Provenance (rule id, method) is not a category change.
func (d Decision) CategoryChanged(t *model.Transaction) bool {
	return d.CategoryID != nil && (t.CategoryID == nil || *t.CategoryID != *d.CategoryID)
}

// HideChanged reports whether applying d would hide t: the rule hides, t is
// a flow row (ledger rows like trades or adjustments are never hidden), t
// isn't a paired transfer leg (transfers are never in spending, so hiding
// one changes nothing; #645) and t isn't hidden yet. A rule never unhides.
func (d Decision) HideChanged(t *model.Transaction) bool {
	return d.Hide && t.HiddenAt == nil && !t.IsTransfer && (t.Kind == "" || t.Kind == model.KindFlow)
}

// ReviewChanged reports whether applying d would mark t reviewed: the rule
// marks reviewed, t is a flow row (only flow rows are in the review inbox)
// and t isn't reviewed yet. A rule never un-reviews.
func (d Decision) ReviewChanged(t *model.Transaction) bool {
	return d.MarkReviewed && t.ReviewedAt == nil && (t.Kind == "" || t.Kind == model.KindFlow)
}

// TagsToAdd returns the rule tags t doesn't carry yet (case-insensitive),
// capped so t stays within model.MaxTagsPerRow. Only flow rows are tagged,
// like hide and review. A rule never removes a tag.
func (d Decision) TagsToAdd(t *model.Transaction) []string {
	if len(d.AddTags) == 0 || (t.Kind != "" && t.Kind != model.KindFlow) {
		return nil
	}
	have := make(map[string]bool, len(t.Tags)+len(d.AddTags))
	for _, tag := range t.Tags {
		have[strings.ToLower(tag)] = true
	}
	room := model.MaxTagsPerRow - len(t.Tags)
	var out []string
	for _, tag := range d.AddTags {
		if len(out) >= room {
			break
		}
		key := strings.ToLower(tag)
		if have[key] {
			continue
		}
		have[key] = true
		out = append(out, tag)
	}
	return out
}

// TagsChanged reports whether applying d would add a tag to t.
func (d Decision) TagsChanged(t *model.Transaction) bool {
	return len(d.TagsToAdd(t)) > 0
}

// SameAs reports whether a row's stored categorization already reflects d:
// the rule's category and the rule as provenance (when the rule sets a
// category), its merchant rename, its hide, its review mark and its tags.
// Re-applying such a row is a no-op.
func (d Decision) SameAs(t *model.Transaction) bool {
	if d.CategoryID != nil && (d.CategoryChanged(t) ||
		t.CategorizationRuleID == nil || *t.CategorizationRuleID != d.RuleID ||
		t.CategorizationMethod == nil || *t.CategorizationMethod != MethodRule) {
		return false
	}
	return !d.MerchantChanged(t) &&
		!d.HideChanged(t) &&
		!d.ReviewChanged(t) &&
		!d.TagsChanged(t)
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
			AccountID:  r.AccountID,
			AmountMin:  r.AmountMin,
			AmountMax:  r.AmountMax,
			patternCI:  strings.ToUpper(r.Pattern),
		}
		if r.RenameMerchant != nil {
			if v := strings.TrimSpace(*r.RenameMerchant); v != "" {
				c.RenameMerchant = &v
			}
		}
		c.Hide = r.HideMatches
		c.MarkReviewed = r.MarkReviewed
		if len(r.AddTags) > 0 {
			c.AddTags = []string(r.AddTags)
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
//
// Categorize knows no account or amount, so rules with an account or amount
// condition (#550) never fire here. Callers holding a transaction use Match.
func Categorize(rules []CompiledRule, assetID int64, descriptionClean, description, merchantName *string) (Decision, bool) {
	return categorize(rules, subject{assetID: assetID}, descriptionClean, description, merchantName)
}

// Match is Categorize for a stored or about-to-be-stored transaction: it
// also checks each rule's account and amount conditions against t.
func Match(rules []CompiledRule, t *model.Transaction) (Decision, bool) {
	if t == nil {
		return Decision{}, false
	}
	amount := t.Amount
	return categorize(rules, subject{assetID: t.AssetID, accountID: t.AccountID, amount: &amount}, t.DescriptionClean, t.Description, t.MerchantName)
}

// subject is the non-text part of what a rule matches against. accountID 0
// and a nil amount mean unknown: rules conditioned on them don't fire.
type subject struct {
	assetID   int64
	accountID int64
	amount    *decimal.Decimal
}

func categorize(rules []CompiledRule, sub subject, descriptionClean, description, merchantName *string) (Decision, bool) {
	if len(rules) == 0 {
		return Decision{}, false
	}
	fields := collectFields(descriptionClean, description, merchantName)
	for _, r := range rules {
		if r.AssetID != nil {
			if sub.assetID == 0 || *r.AssetID != sub.assetID {
				continue
			}
		}
		if !r.conditionsMatch(sub) {
			continue
		}
		// Asset-bound rules with no pattern match purely on asset_id —
		// useful for "all AAPL legs → Investments". collectFields can
		// return empty for trade legs (which have no merchant text);
		// don't reject them up-front.
		if len(fields) == 0 {
			if r.AssetID != nil && r.Pattern == "" {
				return r.decision(), true
			}
			continue
		}
		if matches(r, fields) {
			return r.decision(), true
		}
	}
	return Decision{}, false
}

// conditionsMatch checks the rule's account and amount conditions (#550).
// Amounts compare the magnitude, so a $15.49 band matches both the charge
// (-15.49) and its refund (+15.49); both bounds are inclusive.
func (r CompiledRule) conditionsMatch(sub subject) bool {
	if r.AccountID != nil && (sub.accountID == 0 || *r.AccountID != sub.accountID) {
		return false
	}
	if r.AmountMin == nil && r.AmountMax == nil {
		return true
	}
	if sub.amount == nil {
		return false
	}
	mag := sub.amount.Abs()
	if r.AmountMin != nil && mag.LessThan(*r.AmountMin) {
		return false
	}
	if r.AmountMax != nil && mag.GreaterThan(*r.AmountMax) {
		return false
	}
	return true
}

func (r CompiledRule) decision() Decision {
	return Decision{RuleID: r.ID, CategoryID: r.CategoryID, RenameMerchant: r.RenameMerchant, Hide: r.Hide, MarkReviewed: r.MarkReviewed, AddTags: r.AddTags}
}

// Apply runs Categorize and, when a rule matches, mutates tx in place to
// reflect the decision: when the rule sets a category, CategoryID,
// CategorizationMethod="rule" and CategorizationRuleID; when it renames,
// hides, marks reviewed or tags, MerchantName, HiddenAt, ReviewedAt and Tags.
// Returns the decision and whether a rule matched. A matched rule without a
// category leaves the row's category to the caller's fallback (#553).
// Caller is responsible for deciding whether to call Apply — typically only
// when the row has no existing category.
func Apply(tx *model.Transaction, rules []CompiledRule) (Decision, bool) {
	if tx == nil {
		return Decision{}, false
	}
	d, ok := Match(rules, tx)
	if !ok {
		return Decision{}, false
	}
	d.SetActions(tx)
	return d, true
}

// SetActions writes the decision onto tx: its category with the rule as
// provenance (when the rule sets one), merchant rename, hide, review mark
// and tags, if any.
func (d Decision) SetActions(tx *model.Transaction) {
	if d.CategoryID != nil {
		catID := *d.CategoryID
		ruleID := d.RuleID
		method := MethodRule
		tx.CategoryID = &catID
		tx.CategorizationRuleID = &ruleID
		tx.CategorizationMethod = &method
	}
	if d.MerchantChanged(tx) {
		v := *d.RenameMerchant
		tx.MerchantName = &v
	}
	now := time.Now().UTC()
	if d.HideChanged(tx) {
		tx.HiddenAt = &now
	}
	if d.ReviewChanged(tx) {
		reviewed := now
		tx.ReviewedAt = &reviewed
	}
	if add := d.TagsToAdd(tx); len(add) > 0 {
		tags := make(model.Tags, 0, len(tx.Tags)+len(add))
		tags = append(tags, tx.Tags...)
		tx.Tags = append(tags, add...)
	}
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
