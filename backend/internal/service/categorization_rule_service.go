package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

var (
	ErrRuleNotFound      = errors.New("categorization rule not found")
	ErrEmptyPattern      = errors.New("pattern must not be empty")
	ErrInvalidMatchType  = errors.New("match_type must be one of: contains, regex, exact")
	ErrInvalidRegex      = errors.New("pattern is not a valid regular expression")
	ErrInvalidPriority   = errors.New("priority must be >= 0")
	ErrUnknownCategory   = errors.New("category does not exist")
	ErrInvalidApplyScope = errors.New("scope must be one of: all, uncategorized, plaid_default")
	ErrRenameTooLong     = errors.New("rename_merchant must be at most 200 characters")
)

// maxRenameMerchant bounds a rule's merchant rename; merchant names are
// display labels, not free text.
const maxRenameMerchant = 200

var validMatchTypes = map[string]struct{}{
	"contains": {},
	"regex":    {},
	"exact":    {},
}

type CreateRuleInput struct {
	Pattern    string
	MatchType  string
	CategoryID int64
	// AssetID, when set, narrows the rule to transactions whose
	// asset_id matches. Either Pattern OR AssetID must be supplied —
	// a rule with neither has nothing to match on.
	AssetID *int64
	// RenameMerchant, when non-blank, is written to merchant_name on every
	// row the rule wins (#534). Blank means no rename.
	RenameMerchant *string
	// HideMatches hides the flow rows the rule wins (#536).
	HideMatches bool
	Priority    int
	IsActive    *bool
}

// UpdateRuleInput is a sparse patch. Nil pointer = leave alone.
// ClearAssetID drops asset binding without losing the difference between
// "leave alone" and "clear" the way a nil AssetID would.
type UpdateRuleInput struct {
	Pattern      *string
	MatchType    *string
	CategoryID   *int64
	AssetID      *int64
	ClearAssetID bool
	// RenameMerchant sets the rename; a blank string clears it (#534).
	RenameMerchant *string
	// HideMatches turns hiding on or off (#536). Turning it off leaves
	// already-hidden rows hidden.
	HideMatches *bool
	Priority    *int
	IsActive    *bool
}

// normRename trims a rename and maps blank to nil (no rename).
func normRename(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

// CategorizationRuleService owns rule validation and CRUD. It depends on
// CategoryRepository only to verify CategoryID points at a real row — rules
// have no cross-user visibility, so no further dependencies are needed.
//
// txRepo + db are optional dependencies used by Apply to bulk re-categorize
// existing transactions against the user's current rules. CRUD continues to
// work without them; if Apply is called and they're nil, it returns
// ErrInvalidApplyScope so the router can fail loudly during wiring.
type CategorizationRuleService struct {
	repo    repository.CategorizationRuleRepository
	catRepo repository.CategoryRepository
	txRepo  repository.TransactionRepository
	db      *gorm.DB
}

func NewCategorizationRuleService(
	repo repository.CategorizationRuleRepository,
	catRepo repository.CategoryRepository,
) *CategorizationRuleService {
	return &CategorizationRuleService{repo: repo, catRepo: catRepo}
}

// WithBulkApply wires the dependencies needed for the Apply (bulk
// re-categorize) endpoint — a transaction repository for the scan and a
// *gorm.DB so each chunk runs in its own DB transaction. Returns the
// receiver for one-line construction in the router.
func (s *CategorizationRuleService) WithBulkApply(txRepo repository.TransactionRepository, db *gorm.DB) *CategorizationRuleService {
	s.txRepo = txRepo
	s.db = db
	return s
}

func (s *CategorizationRuleService) Create(ctx context.Context, userID int64, in CreateRuleInput) (*model.CategorizationRule, error) {
	r := &model.CategorizationRule{
		UserID:         userID,
		Pattern:        strings.TrimSpace(in.Pattern),
		MatchType:      strings.TrimSpace(in.MatchType),
		CategoryID:     in.CategoryID,
		AssetID:        in.AssetID,
		Priority:       in.Priority,
		IsActive:       true,
		RenameMerchant: normRename(in.RenameMerchant),
		HideMatches:    in.HideMatches,
	}
	if in.IsActive != nil {
		r.IsActive = *in.IsActive
	}
	if err := s.validate(ctx, r); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, r); err != nil {
		return nil, fmt.Errorf("create rule: %w", err)
	}
	return r, nil
}

func (s *CategorizationRuleService) Get(ctx context.Context, userID, id int64) (*model.CategorizationRule, error) {
	r, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	return r, nil
}

func (s *CategorizationRuleService) List(ctx context.Context, userID int64) ([]model.CategorizationRule, error) {
	return s.repo.List(ctx, userID)
}

func (s *CategorizationRuleService) Update(ctx context.Context, userID, id int64, in UpdateRuleInput) (*model.CategorizationRule, error) {
	r, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	if in.Pattern != nil {
		r.Pattern = strings.TrimSpace(*in.Pattern)
	}
	if in.MatchType != nil {
		r.MatchType = strings.TrimSpace(*in.MatchType)
	}
	if in.CategoryID != nil {
		r.CategoryID = *in.CategoryID
	}
	switch {
	case in.ClearAssetID:
		r.AssetID = nil
	case in.AssetID != nil:
		r.AssetID = in.AssetID
	}
	if in.RenameMerchant != nil {
		r.RenameMerchant = normRename(in.RenameMerchant)
	}
	if in.HideMatches != nil {
		r.HideMatches = *in.HideMatches
	}
	if in.Priority != nil {
		r.Priority = *in.Priority
	}
	if in.IsActive != nil {
		r.IsActive = *in.IsActive
	}
	if err := s.validate(ctx, r); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, r); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("update rule: %w", err)
	}
	return r, nil
}

func (s *CategorizationRuleService) SoftDelete(ctx context.Context, userID, id int64) error {
	if err := s.repo.SoftDelete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrRuleNotFound
		}
		return fmt.Errorf("soft delete rule: %w", err)
	}
	return nil
}

// ApplyResult summarizes a bulk-apply run.
type ApplyResult struct {
	Scanned       int `json:"scanned"`
	Updated       int `json:"updated"`
	SkippedManual int `json:"skipped_manual"`
	// DryRun is true when nothing was written; Updated then counts the rows
	// that would change.
	DryRun bool `json:"dry_run"`
}

// Apply walks the user's transactions in chunks and re-runs the categorization
// engine against the current rule set. The scope filters which rows are
// scanned (see repository.CategorizationScope* constants); within the scanned
// set, rows whose categorization_method is 'manual' are always skipped (user
// picks are sacred). A row is updated only when a rule matches AND the
// resulting decision differs from the row's current state — re-runs are
// effectively idempotent.
func (s *CategorizationRuleService) Apply(ctx context.Context, userID int64, scope string) (ApplyResult, error) {
	return s.apply(ctx, userID, scope, false)
}

// Preview is Apply without writes: it reports how many rows the current rule
// set would re-categorize so the user can see the impact before committing.
func (s *CategorizationRuleService) Preview(ctx context.Context, userID int64, scope string) (ApplyResult, error) {
	return s.apply(ctx, userID, scope, true)
}

func (s *CategorizationRuleService) apply(ctx context.Context, userID int64, scope string, dryRun bool) (ApplyResult, error) {
	if scope == "" {
		scope = repository.CategorizationScopeAll
	}
	switch scope {
	case repository.CategorizationScopeAll,
		repository.CategorizationScopeUncategorized,
		repository.CategorizationScopePlaidDefault:
	default:
		return ApplyResult{}, ErrInvalidApplyScope
	}
	if s.txRepo == nil || s.db == nil {
		// Wiring bug — Apply was called on a service without WithBulkApply.
		// Return ErrInvalidApplyScope rather than panicking so the handler maps
		// it to a 400; the router should always call WithBulkApply.
		return ApplyResult{}, ErrInvalidApplyScope
	}

	stored, err := s.repo.List(ctx, userID)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("load rules: %w", err)
	}
	rules := categorization.Compile(stored)

	result := ApplyResult{DryRun: dryRun}
	const chunkSize = 1000
	afterID := int64(0)

	for {
		batch, err := s.txRepo.ListForCategorizationScope(ctx, userID, scope, afterID, chunkSize)
		if err != nil {
			return result, fmt.Errorf("scan transactions: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		// Wrap each chunk in its own DB transaction so a mid-chunk failure
		// commits prior chunks' progress. Per-row Update lives behind a
		// tx-bound repo so all writes hit the same Postgres transaction.
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			txRepo := repository.NewTransactionRepository(tx)
			for i := range batch {
				row := &batch[i]
				result.Scanned++
				afterID = row.ID

				if row.CategorizationMethod != nil && *row.CategorizationMethod == "manual" {
					result.SkippedManual++
					continue
				}
				if len(rules) == 0 {
					continue
				}
				d, ok := categorization.Categorize(rules, row.AssetID, row.DescriptionClean, row.Description, row.MerchantName)
				if !ok {
					continue
				}
				// No-op if the row already reflects this decision.
				if d.SameAs(row) {
					continue
				}
				if dryRun {
					result.Updated++
					continue
				}
				catID := d.CategoryID
				ruleID := d.RuleID
				method := categorization.MethodRule
				row.CategoryID = &catID
				row.CategorizationRuleID = &ruleID
				row.CategorizationMethod = &method
				d.SetActions(row)
				if err := txRepo.Update(ctx, row); err != nil {
					return fmt.Errorf("update transaction %d: %w", row.ID, err)
				}
				result.Updated++
			}
			return nil
		})
		if err != nil {
			return result, err
		}
		if len(batch) < chunkSize {
			break
		}
	}
	return result, nil
}

// DraftRuleInput is an unsaved rule to evaluate. RuleID, when set, means the
// draft replaces that stored rule (edit); otherwise it is a new rule.
type DraftRuleInput struct {
	RuleID     *int64
	Pattern    string
	MatchType  string
	CategoryID int64
	AssetID    *int64
	// RenameMerchant as on CreateRuleInput (#534).
	RenameMerchant *string
	// HideMatches as on CreateRuleInput (#536).
	HideMatches bool
	Priority    int
	IsActive    bool
}

// DraftPreview reports a draft rule's impact against the user's existing
// transactions without writing anything.
type DraftPreview struct {
	Scanned int `json:"scanned"`
	// Matched counts rows where the draft is the winning rule (after priority
	// ordering against the user's other rules), manual picks excluded.
	Matched int `json:"matched"`
	// WouldChange is the subset of Matched whose stored category, rule,
	// merchant (when the draft renames) or hidden state (when it hides) differs.
	WouldChange int `json:"would_change"`
	// WouldRename is the subset of Matched whose merchant the draft's rename
	// would change (#534).
	WouldRename int `json:"would_rename"`
	// WouldHide is the subset of Matched the draft would hide (#536).
	WouldHide     int `json:"would_hide"`
	SkippedManual int `json:"skipped_manual"`
	// Samples are up to draftSampleLimit descriptions of matched rows.
	Samples []string `json:"samples"`
}

const draftSampleLimit = 5

// PreviewDraft validates the draft like Create/Update would, merges it into
// the user's rule set at its priority, and reports which transactions it would
// win. Nothing is written.
func (s *CategorizationRuleService) PreviewDraft(ctx context.Context, userID int64, in DraftRuleInput) (DraftPreview, error) {
	if s.txRepo == nil {
		return DraftPreview{}, ErrInvalidApplyScope
	}
	draft := &model.CategorizationRule{
		UserID:         userID,
		Pattern:        strings.TrimSpace(in.Pattern),
		MatchType:      strings.TrimSpace(in.MatchType),
		CategoryID:     in.CategoryID,
		AssetID:        in.AssetID,
		Priority:       in.Priority,
		IsActive:       in.IsActive,
		ID:             math.MaxInt64, // new rules get the highest id, so lose priority ties
		RenameMerchant: normRename(in.RenameMerchant),
		HideMatches:    in.HideMatches,
	}
	if in.RuleID != nil {
		if _, err := s.repo.GetByID(ctx, userID, *in.RuleID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return DraftPreview{}, ErrRuleNotFound
			}
			return DraftPreview{}, err
		}
		draft.ID = *in.RuleID
	}
	if err := s.validate(ctx, draft); err != nil {
		return DraftPreview{}, err
	}
	stored, err := s.repo.List(ctx, userID)
	if err != nil {
		return DraftPreview{}, fmt.Errorf("load rules: %w", err)
	}
	merged := make([]model.CategorizationRule, 0, len(stored)+1)
	for _, r := range stored {
		if in.RuleID != nil && r.ID == *in.RuleID {
			continue
		}
		merged = append(merged, r)
	}
	merged = append(merged, *draft)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Priority != merged[j].Priority {
			return merged[i].Priority > merged[j].Priority
		}
		return merged[i].ID < merged[j].ID
	})
	rules := categorization.Compile(merged)

	out := DraftPreview{Samples: []string{}}
	const chunkSize = 1000
	afterID := int64(0)
	for {
		batch, err := s.txRepo.ListForCategorizationScope(ctx, userID, repository.CategorizationScopeAll, afterID, chunkSize)
		if err != nil {
			return out, fmt.Errorf("scan transactions: %w", err)
		}
		for i := range batch {
			row := &batch[i]
			out.Scanned++
			afterID = row.ID
			if row.CategorizationMethod != nil && *row.CategorizationMethod == "manual" {
				out.SkippedManual++
				continue
			}
			d, ok := categorization.Categorize(rules, row.AssetID, row.DescriptionClean, row.Description, row.MerchantName)
			if !ok || d.RuleID != draft.ID {
				continue
			}
			out.Matched++
			if len(out.Samples) < draftSampleLimit {
				out.Samples = append(out.Samples, draftSampleText(row))
			}
			if d.MerchantChanged(row.MerchantName) {
				out.WouldRename++
			}
			if d.HideChanged(row) {
				out.WouldHide++
			}
			if d.SameAs(row) {
				continue
			}
			out.WouldChange++
		}
		if len(batch) < chunkSize {
			break
		}
	}
	return out, nil
}

func draftSampleText(t *model.Transaction) string {
	for _, p := range []*string{t.MerchantName, t.DescriptionClean, t.Description} {
		if p != nil && strings.TrimSpace(*p) != "" {
			return strings.TrimSpace(*p)
		}
	}
	return "(no description)"
}

func (s *CategorizationRuleService) validate(ctx context.Context, r *model.CategorizationRule) error {
	// A rule needs *something* to match on. Asset-only rules are valid
	// (e.g. "every AAPL leg → Investments"); pattern-only rules are the
	// original M4 shape. A rule with neither has nothing to match.
	if r.Pattern == "" && r.AssetID == nil {
		return ErrEmptyPattern
	}
	// MatchType is still required when there's a pattern; ignored when
	// the rule is purely asset-bound.
	if r.Pattern != "" {
		if _, ok := validMatchTypes[r.MatchType]; !ok {
			return ErrInvalidMatchType
		}
		if r.MatchType == "regex" {
			if _, err := regexp.Compile(r.Pattern); err != nil {
				return ErrInvalidRegex
			}
		}
	}
	if r.Priority < 0 {
		return ErrInvalidPriority
	}
	if r.RenameMerchant != nil && len([]rune(*r.RenameMerchant)) > maxRenameMerchant {
		return ErrRenameTooLong
	}
	if _, err := s.catRepo.GetByID(ctx, r.CategoryID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrUnknownCategory
		}
		return fmt.Errorf("validate category: %w", err)
	}
	return nil
}
