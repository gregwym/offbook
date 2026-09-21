package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// CategorizationPassConfig tunes one call to RunCategorizationPass. Zero
// values are invalid (ConfidenceThreshold=0 would commit everything,
// BatchSize=0 would never call the categorizer) — callers pass the
// config-derived defaults (ADR-0022 §7).
type CategorizationPassConfig struct {
	// ConfidenceThreshold is the minimum verdict confidence that gets
	// written to a row. Below it, the row is left exactly as it was — still
	// visible to the existing "Needs review" flow.
	ConfidenceThreshold float64
	// BatchSize is how many candidates go into one Categorizer.Categorize
	// call.
	BatchSize int
}

// CategorizationPassResult summarizes one user's pass, for job logging.
type CategorizationPassResult struct {
	Scanned         int
	CacheHits       int
	AICalls         int
	Categorized     int
	LowConfidence   int
	BudgetExhausted bool
}

// categorizationRow pairs a scanned transaction with its normalized merchant
// key, so both travel together through the cache-lookup → queue → write
// pipeline below without recomputing the key.
type categorizationRow struct {
	row *model.Transaction
	key string
}

// RunCategorizationPass is the per-user AI transaction-categorization batch
// pass (#366, ADR-0022). It scans CategorizationScopeAIEligible rows (never
// manual/rule rows — see ADR-0022 §2), resolves each row's merchant key, and:
//  1. On a cache hit at/above threshold: writes the row for free (no AI call).
//  2. On a cache hit below threshold: leaves the row alone (no repeat call).
//  3. On a cache miss: queues the row into an AI batch.
//
// Queued candidates are sent to categorizer in batches of cfg.BatchSize.
// Each batch call decrements *remainingBudget by one; when the budget hits
// zero, remaining candidates (in this user's scan and any users not yet
// processed in the same job run) are left untouched — "stay uncategorized
// until tomorrow" per ADR-0022 §7. remainingBudget is shared across a whole
// job run (the caller passes the same pointer for every user); it must not
// be nil.
//
// categorizer may be nil (no provider configured for this user, or the
// protocol isn't supported yet — ADR-0022 §2): the pass still applies cache
// hits, it just never queues an AI batch.
func RunCategorizationPass(
	ctx context.Context,
	db *gorm.DB,
	txRepo repository.TransactionRepository,
	catRepo repository.CategoryRepository,
	verdictRepo repository.AICategorizationVerdictRepository,
	categorizer categorization.Categorizer,
	userID int64,
	cfg CategorizationPassConfig,
	remainingBudget *int,
) (CategorizationPassResult, error) {
	var res CategorizationPassResult
	if db == nil || txRepo == nil || catRepo == nil || verdictRepo == nil {
		return res, errors.New("service: RunCategorizationPass requires non-nil db/txRepo/catRepo/verdictRepo")
	}
	if remainingBudget == nil {
		return res, errors.New("service: RunCategorizationPass requires a non-nil remainingBudget")
	}
	if cfg.ConfidenceThreshold <= 0 || cfg.BatchSize <= 0 {
		return res, errors.New("service: RunCategorizationPass requires ConfidenceThreshold > 0 and BatchSize > 0")
	}

	categories, err := catRepo.List(ctx, userID)
	if err != nil {
		return res, fmt.Errorf("categorization pass: list categories: %w", err)
	}
	idBySlug := make(map[string]int64, len(categories))
	options := make([]categorization.CategoryOption, 0, len(categories))
	for _, c := range categories {
		idBySlug[c.Slug] = c.ID
		options = append(options, categorization.CategoryOption{ID: c.ID, Slug: c.Slug})
	}

	const chunkSize = 500
	afterID := int64(0)

	for {
		batch, err := txRepo.ListForCategorizationScope(ctx, userID, repository.CategorizationScopeAIEligible, afterID, chunkSize)
		if err != nil {
			return res, fmt.Errorf("categorization pass: scan transactions: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		var toWrite []*model.Transaction // resolved via cache hit, ready to write
		var toQueue []categorizationRow  // cache miss, needs an AI verdict
		for i := range batch {
			row := &batch[i]
			res.Scanned++
			afterID = row.ID

			key := categorization.NormalizeMerchantKey(row.MerchantName, row.DescriptionClean)
			if key == "" {
				continue
			}

			cached, err := verdictRepo.Get(ctx, userID, key)
			if err == nil {
				res.CacheHits++
				if cached.Confidence >= cfg.ConfidenceThreshold {
					applyVerdict(row, cached.CategoryID)
					toWrite = append(toWrite, row)
				} else {
					res.LowConfidence++
				}
				continue
			}
			if !errors.Is(err, repository.ErrNotFound) {
				return res, fmt.Errorf("categorization pass: verdict cache lookup: %w", err)
			}
			if categorizer == nil {
				continue // no provider — leave uncategorized, nothing more to do
			}
			toQueue = append(toQueue, categorizationRow{row: row, key: key})
		}

		// Cache-hit writes cost no budget — apply them regardless of
		// remaining budget.
		if err := writeRows(ctx, db, toWrite); err != nil {
			return res, err
		}
		res.Categorized += len(toWrite)

		// AI batches: consume the shared budget, one unit per Categorize call.
		for start := 0; start < len(toQueue) && *remainingBudget > 0; start += cfg.BatchSize {
			end := start + cfg.BatchSize
			if end > len(toQueue) {
				end = len(toQueue)
			}
			sub := toQueue[start:end]

			candidates := make([]categorization.Candidate, len(sub))
			for i, p := range sub {
				candidates[i] = categorization.Candidate{
					Ref:              i,
					MerchantName:     p.row.MerchantName,
					DescriptionClean: p.row.DescriptionClean,
					AmountSign:       amountSign(p.row),
				}
			}
			verdicts, callErr := categorizer.Categorize(ctx, candidates, options)
			*remainingBudget--
			res.AICalls++
			if callErr != nil {
				// A provider failure for one batch shouldn't abort the whole
				// pass (other users, other batches) — the rows involved
				// simply stay uncategorized and get retried tomorrow.
				continue
			}

			var batchWrite []*model.Transaction
			for _, v := range verdicts {
				if v.Ref < 0 || v.Ref >= len(sub) {
					continue
				}
				p := sub[v.Ref]
				catID, ok := idBySlug[v.CategorySlug]
				if !ok {
					continue
				}
				if err := verdictRepo.Upsert(ctx, &model.AICategorizationVerdict{
					UserID:      userID,
					MerchantKey: p.key,
					CategoryID:  catID,
					Confidence:  v.Confidence,
					Provider:    categorizer.Name(),
				}); err != nil {
					return res, fmt.Errorf("categorization pass: upsert verdict cache: %w", err)
				}
				if v.Confidence >= cfg.ConfidenceThreshold {
					applyVerdict(p.row, catID)
					batchWrite = append(batchWrite, p.row)
				} else {
					res.LowConfidence++
				}
			}
			if err := writeRows(ctx, db, batchWrite); err != nil {
				return res, err
			}
			res.Categorized += len(batchWrite)
		}
		if *remainingBudget <= 0 && len(toQueue) > 0 {
			res.BudgetExhausted = true
		}

		if len(batch) < chunkSize {
			break
		}
	}
	return res, nil
}

// applyVerdict mutates row in place to reflect a confident AI verdict
// (cached or freshly returned) — categorization_method='ai', per ADR-0022.
func applyVerdict(row *model.Transaction, categoryID int64) {
	method := "ai"
	row.CategoryID = &categoryID
	row.CategorizationMethod = &method
	row.CategorizationRuleID = nil
}

func amountSign(t *model.Transaction) string {
	if t.Amount.IsNegative() {
		return categorization.AmountSignDebit
	}
	return categorization.AmountSignCredit
}

// writeRows persists every row's current in-memory state in one DB
// transaction — mirrors CategorizationRuleService.Apply's per-chunk
// transaction wrapping.
func writeRows(ctx context.Context, db *gorm.DB, rows []*model.Transaction) error {
	if len(rows) == 0 {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		for _, row := range rows {
			if err := txRepo.Update(ctx, row); err != nil {
				return fmt.Errorf("update transaction %d: %w", row.ID, err)
			}
		}
		return nil
	})
}
