package repository

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gregwym/offbook/backend/internal/model"
)

// TransactionFilter narrows the set of transactions returned by List.
// Zero values mean "no filter on that dimension"; nil pointers mean the same.
// UncategorizedOnly takes precedence over CategoryID — set at most one.
type TransactionFilter struct {
	AccountID         *int64
	CategoryID        *int64
	UncategorizedOnly bool // matches rows where category_id IS NULL
	// CategorizationMethod, when non-empty, restricts to rows where
	// categorization_method matches exactly. Used by the v6 "Needs review"
	// filter chip to surface rows auto-assigned via plaid_default that the
	// user hasn't confirmed or rule-overridden.
	CategorizationMethod string
	// Reviewed, when non-nil, restricts to reviewed (true) or unreviewed
	// (false) rows (#424). Unreviewed is restricted to kind=flow: reconciling
	// rows and trade legs are system-written and never enter the inbox.
	Reviewed *bool
	From     *time.Time // inclusive lower bound on transaction_date
	To       *time.Time // inclusive upper bound on transaction_date
	Search   string     // ILIKE %term% across description + merchant_name
	// Tag, when non-empty, restricts to rows carrying this exact tag (#544).
	Tag string
	// Spending switches to the spending lens (#571): only rows with a
	// spend_lines allocation that the spending figures count (flow, not a
	// transfer or hidden, outflow or refund). CategoryID/UncategorizedOnly
	// then match the allocation's category, so a split row appears under
	// each category it is split into.
	Spending bool
	// Merchant, with Spending, restricts to rows whose merchant label
	// (merchantLabelSQL) equals it exactly — the Top merchants grouping.
	Merchant string
	Limit    int
	Offset   int
}

// merchantLabelSQL is the merchant label Top merchants groups by, over
// transactions aliased m.
const merchantLabelSQL = `COALESCE(NULLIF(m.merchant_name, ''), NULLIF(m.description_clean, ''), m.description, 'Unknown')`

// spendLineCond is the spending-lens predicate over spend_lines aliased s
// (joined to its transaction as m) — the same rows SpendByCategory,
// TopMerchants and budget spend sum (#571).
func spendLineCond(userID int64, f TransactionFilter) (string, []any) {
	cond := `s.user_id = ? AND s.deleted_at IS NULL AND s.is_transfer = FALSE
		AND s.kind = 'flow' AND (s.amount < 0 OR s.is_refund)`
	args := []any{userID}
	switch {
	case f.UncategorizedOnly:
		cond += " AND s.category_id IS NULL"
	case f.CategoryID != nil:
		cond += " AND s.category_id = ?"
		args = append(args, *f.CategoryID)
	}
	if f.Merchant != "" {
		cond += " AND " + merchantLabelSQL + " = ?"
		args = append(args, f.Merchant)
	}
	return cond, args
}

// TransactionRepository is the data-access contract for transactions.
// All read paths are scoped by user_id.
type TransactionRepository interface {
	Create(ctx context.Context, t *model.Transaction) error
	GetByID(ctx context.Context, userID, id int64) (*model.Transaction, error)
	List(ctx context.Context, userID int64, f TransactionFilter) ([]model.Transaction, int64, error)
	// ListTags returns the user's distinct tags on live rows with how many
	// rows carry each, alphabetical (case-insensitive) (#544).
	ListTags(ctx context.Context, userID int64) ([]TagCount, error)
	// SpendTotal sums the spending-lens allocations of List(f)'s rows (#571).
	SpendTotal(ctx context.Context, userID int64, f TransactionFilter) (decimal.Decimal, error)
	Update(ctx context.Context, t *model.Transaction) error
	SoftDelete(ctx context.Context, userID, id int64) error
	// CreateBatch inserts many transactions in one round-trip, doing
	// nothing on conflict with the (plaid_transaction_id) unique index.
	// Returns the number of rows actually inserted — caller compares that
	// to len(txns) to derive "skipped as duplicate" if needed.
	CreateBatch(ctx context.Context, txns []model.Transaction) (int64, error)
	// UpdateByPlaidTransactionID overwrites a row keyed by user_id +
	// plaid_transaction_id. The caller supplies the post-merge row; this
	// repo does not enforce field-level merge semantics — see
	// plaid.MergePlaidUpdate for the policy.
	// Returns ErrNotFound if no matching row exists (e.g. a `modified`
	// for a transaction we never saw because it was added+modified+removed
	// across syncs).
	UpdateByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error
	// SoftDeleteByPlaidTransactionID flips deleted_at for a transaction
	// matched by user_id + plaid_transaction_id. Returns ErrNotFound when
	// no live row exists. Idempotent: re-applying on an already-deleted
	// row returns ErrNotFound (because the soft-delete partial index
	// hides the row from the scope).
	SoftDeleteByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string) error
	// FindSoftDeletedByPlaidTransactionIDs returns rows whose deleted_at IS
	// NOT NULL and whose (user_id, plaid_transaction_id) matches one of the
	// supplied IDs. Used by the Plaid sync path to detect a re-surfaced
	// transaction that the user (or a prior sync) had previously soft-deleted
	// — instead of inserting a duplicate, the service resurrects the row.
	// Empty input → nil, nil.
	FindSoftDeletedByPlaidTransactionIDs(ctx context.Context, userID int64, plaidTxnIDs []string) ([]model.Transaction, error)
	// ResurrectByPlaidTransactionID clears deleted_at on a soft-deleted row
	// and overlays the merged fields. Returns ErrNotFound when no row exists
	// (live or soft-deleted) for the key. The caller is expected to pass a
	// row produced by plaid.MergePlaidUpdate so user-edited fields survive.
	ResurrectByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error
	// CreateTradePair inserts two paired transaction rows in one DB
	// transaction, generating a shared transfer_pair_id pointing at both
	// rows' IDs. Used by manual trade entry and Plaid investment-
	// transactions ingestion (#238). Both legs must already carry the
	// same UserID; the repo does NOT enforce that the pair makes
	// accounting sense (signs/quantities/assets) — that's the service's
	// job. On success the supplied legs are populated with their assigned
	// IDs and the shared TransferPairID.
	//
	// The pairing scheme stores legA.TransferPairID = legB.ID and
	// legB.TransferPairID = legA.ID — symmetric, so either side can find
	// its partner with one indexed lookup.
	CreateTradePair(ctx context.Context, legA, legB *model.Transaction) error
	// ListByTransferPairID returns the two (or rarely one — e.g. mid-
	// migration) live transactions sharing a transfer_pair_id, scoped to
	// the supplied user. Returns ErrNotFound when neither row exists.
	ListByTransferPairID(ctx context.Context, userID, pairID int64) ([]model.Transaction, error)
	// ListByAccountAndAsset returns every non-deleted transaction for the
	// (account, asset) pair ordered by transaction_date ASC, id ASC. Used
	// by the cost-basis recompute, which walks the entire trade history
	// per (account, asset) and folds quantities + cash legs together.
	// Takes the position lock first (see lockPosition).
	ListByAccountAndAsset(ctx context.Context, userID, accountID, assetID int64) ([]model.Transaction, error)
	// FoldQuantity returns Σ amount over non-deleted transactions for the
	// (account, asset) pair — the quantity the position should equal
	// (ADR-0017). Returns 0 when there are no rows. Takes the position lock
	// first (see lockPosition).
	FoldQuantity(ctx context.Context, userID, accountID, assetID int64) (decimal.Decimal, error)
	// HasReconcilingTxn reports whether an opening_balance or adjustment row
	// already exists for the (account, asset) pair. The first reconciling
	// write is an opening_balance anchor; later ones are adjustments.
	HasReconcilingTxn(ctx context.Context, userID, accountID, assetID int64) (bool, error)
	// ListReconciling returns every opening_balance/adjustment row for the
	// account (across all assets it holds), oldest first — the per-account
	// reconciliation view's (#370) ledger-side half.
	ListReconciling(ctx context.Context, userID, accountID int64) ([]model.Transaction, error)
	// FoldQuantityBefore returns the (account, asset) fold using only rows
	// strictly before (beforeDate, beforeID) in (transaction_date, id) order
	// — i.e. excluding the row at that position and anything dated on/after
	// it. Used by the reconciliation view (#370) to reconstruct "quantity
	// immediately before this opening_balance/adjustment row was applied",
	// independent of any observation link (robust even for legacy rows
	// written before caused_by_observation_id existed).
	FoldQuantityBefore(ctx context.Context, userID, accountID, assetID int64, beforeDate time.Time, beforeID int64) (decimal.Decimal, error)
	// ReplaceSplits atomically replaces the category splits of one owned
	// transaction (#438). An empty slice clears them. Returns ErrNotFound when
	// the transaction is not the user's / is deleted.
	ReplaceSplits(ctx context.Context, userID, txnID int64, splits []model.TransactionSplit) error
	// SetReviewed sets (reviewedAt non-nil) or clears (nil) reviewed_at on the
	// given rows, scoped to the user, and returns the number of rows changed.
	// Ids belonging to other users / deleted rows are silently not matched.
	SetReviewed(ctx context.Context, userID int64, ids []int64, reviewedAt *time.Time) (int64, error)
	// BulkEdit applies one edit to the user's live flow rows among ids in a
	// single DB transaction (#567) and returns the prior state of every row it
	// actually changed, so the edit can be undone. Ids of other users, deleted
	// or non-flow rows are not matched. A category edit skips split parents
	// (their category lives on the lines) and reports how many it skipped.
	BulkEdit(ctx context.Context, userID int64, ids []int64, edit BulkEdit) ([]BulkEditPrior, int, error)
	// BulkRestore writes each prior state back in one DB transaction (#567),
	// skipping rows modified since the edit (updated_at no longer equals
	// prior.UpdatedAt) and deleted rows. A rule id the user doesn't own is
	// dropped. Returns the rows restored.
	BulkRestore(ctx context.Context, userID int64, prior []BulkEditPrior) (int, error)
	// AcknowledgeAdjustment marks an adjustment transaction reviewed (#370)
	// without altering ledger fields (amount/kind/quantity facts are
	// untouched — same category as the existing Notes field). Only rows with
	// kind=adjustment are acknowledgeable: opening_balance is the expected
	// day-0 anchor, never "unexplained". Returns ErrNotFound if no such row
	// exists for the user.
	AcknowledgeAdjustment(ctx context.Context, userID, transactionID int64, note *string) (*model.Transaction, error)
	// ListUnacknowledgedAdjustments returns every live kind=adjustment row
	// for the user with acknowledged_at IS NULL — the input to the
	// per-account "needs attention" flag (#370), computed once per request
	// across every account rather than per-account.
	ListUnacknowledgedAdjustments(ctx context.Context, userID int64) ([]model.Transaction, error)
	// DistinctAccountAssetPairs returns every (account_id, asset_id) pair that
	// has at least one non-deleted transaction for the user. Used by the
	// positions rebuild to materialize positions.quantity = Σ amount per pair
	// (ADR-0017) — proving positions is a pure fold of the ledger.
	DistinctAccountAssetPairs(ctx context.Context, userID int64) ([]AccountAssetPair, error)
	// FoldByUserAsOf returns one synthetic position per asset for the user:
	// quantity = Σ non-deleted transactions.amount with transaction_date <=
	// asOf, across all the user's accounts. Assets folding to zero are
	// omitted. Used by the unified net-worth trend (#282) to reconstruct the
	// quantity held at a past instant from the ledger. AccountID is left zero
	// — the caller values per asset in a single quote currency.
	//
	// An opening_balance row is the day-0 anchor (ADR-0017): it counts from the
	// earliest dated row of its own (account, asset), not from the day it was
	// written, so months before a link/statement date include the starting
	// balance instead of just the flows (#528).
	FoldByUserAsOf(ctx context.Context, userID int64, asOf time.Time) ([]model.Position, error)
	// ListForCategorizationScope returns a chunk of non-deleted transactions
	// for the user, ordered by id ASC for cursor-style pagination. Callers
	// drive a loop by passing the last returned row's id as afterID on the
	// next call. The empty slice signals end-of-scan.
	//
	// scope filters the underlying query:
	//   - "all"           — every non-deleted transaction
	//   - "uncategorized" — category_id IS NULL
	//   - "plaid_default" — categorization_method = 'plaid_default'
	//
	// Manual rows (categorization_method = 'manual') are NOT filtered out
	// here — the bulk-apply service inspects each row so it can count
	// "skipped_manual" for the response. For scope='plaid_default' the
	// method filter inherently excludes manual rows.
	ListForCategorizationScope(ctx context.Context, userID int64, scope string, afterID int64, limit int) ([]model.Transaction, error)
	// ExistingExternalIDs returns the subset of the supplied external_ids that
	// already have a LIVE (deleted_at IS NULL) transaction for the (user,
	// account) pair — matching the uq_transactions_external partial index. Used
	// by CSV import (#330) to classify rows as new vs duplicate before insert.
	// Empty input → empty set.
	ExistingExternalIDs(ctx context.Context, userID, accountID int64, externalIDs []string) (map[string]struct{}, error)
	// ImportBatch inserts CSV-sourced transactions in one round-trip, doing
	// nothing on conflict with the (account_id, external_id) partial unique
	// index uq_transactions_external. Returns the number of rows inserted —
	// the caller compares against len(txns) to count duplicates skipped by a
	// concurrent import. Like CreateBatch, this only guards LIVE duplicates;
	// soft-deleted rows sit outside the partial index.
	ImportBatch(ctx context.Context, txns []model.Transaction) (int64, error)
	// ImportJobAssetIDs returns the distinct assets of the live rows the job
	// created (flow rows plus its reconciling adjustments). User-scoped.
	ImportJobAssetIDs(ctx context.Context, userID, jobID int64) ([]int64, error)
	// SoftDeleteByImportJob soft-deletes every live row created by the job
	// (flow rows and reconciling adjustments) and returns how many were
	// removed. User-scoped.
	SoftDeleteByImportJob(ctx context.Context, userID, jobID int64) (int64, error)
	// CountLiveByImportJobs returns, per job id, the live rows it created.
	CountLiveByImportJobs(ctx context.Context, userID int64, jobIDs []int64) (map[int64]int64, error)
	// ListTransferCandidates is the deterministic matcher for #371: it finds
	// live kind=flow transactions for userID that are not already a transfer,
	// pairs each with the closest-by-date opposite-amount, same-asset
	// transaction in a DIFFERENT account within windowDays, and returns each
	// live transaction in at most one pair (closest date wins; lower id
	// breaks ties). Same-asset only — cross-currency matching needs FX
	// inference and is deferred (see issue #371 "Out of Scope"). Pure read;
	// nothing is persisted or linked.
	ListTransferCandidates(ctx context.Context, userID int64, windowDays int) ([]TransferCandidate, error)
	// LinkTransferPair sets is_transfer=true and a symmetric transfer_pair_id
	// (A→B.ID, B→A.ID — same scheme as CreateTradePair) on two existing live
	// transactions scoped to userID. The repo only enforces user ownership and
	// row existence; the caller (TransferMatchingService) enforces the
	// accounting rules (different accounts, kind=flow, opposite amounts, not
	// already paired). Wrapped in one DB transaction — see database.md on
	// multi-row writes.
	LinkTransferPair(ctx context.Context, userID, idA, idB int64) (*model.Transaction, *model.Transaction, error)
	// UnlinkTransferPair clears is_transfer+transfer_pair_id on transactionID
	// and its linked partner (if the partner is still live), scoped to userID.
	// Returns ErrNotFound if transactionID doesn't exist or isn't currently
	// linked as a transfer.
	UnlinkTransferPair(ctx context.Context, userID, transactionID int64) error
	// DismissTransferPair records that idA and idB are "not a transfer"
	// (#510): ListTransferCandidates and spend_lines stop proposing the pair.
	// Both rows must be live and owned by userID (else ErrNotFound); the
	// ownership check and insert share one DB transaction. Idempotent.
	DismissTransferPair(ctx context.Context, userID, idA, idB int64) error
}

// TransferCandidate pairs two live, unmatched kind=flow transactions proposed
// by ListTransferCandidates as the same real-world transfer. Confirming a
// candidate (TransferMatchingService.Confirm) is what actually links them —
// nothing here is auto-applied.
type TransferCandidate struct {
	A model.Transaction
	B model.Transaction
}

// CategorizationScopes enumerates the valid `scope` values for
// ListForCategorizationScope and the bulk re-categorize endpoint.
const (
	CategorizationScopeAll           = "all"
	CategorizationScopeUncategorized = "uncategorized"
	CategorizationScopePlaidDefault  = "plaid_default"
	// CategorizationScopeAIEligible selects the set the AI batch pass (#366,
	// ADR-0022 §2) is allowed to touch: uncategorized OR plaid_default rows,
	// restricted to kind='flow' (trade legs / opening balances / adjustments
	// have no merchant text and aren't spending categorization targets).
	// Manual and rule-categorized rows are structurally excluded — this is
	// how "AI never overrules a choice I or my rules made" is enforced.
	CategorizationScopeAIEligible = "ai_eligible"
)

type transactionRepo struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) TransactionRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) Create(ctx context.Context, t *model.Transaction) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *transactionRepo) GetByID(ctx context.Context, userID, id int64) (*model.Transaction, error) {
	var t model.Transaction
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows := []model.Transaction{t}
	if err := r.attachSplits(ctx, rows); err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// attachSplits fills Splits on each row with one query (#438).
func (r *transactionRepo) attachSplits(ctx context.Context, rows []model.Transaction) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int64, len(rows))
	idx := make(map[int64]int, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
		idx[rows[i].ID] = i
	}
	var splits []model.TransactionSplit
	if err := r.db.WithContext(ctx).Where("transaction_id IN ?", ids).Order("id ASC").Find(&splits).Error; err != nil {
		return err
	}
	for _, sp := range splits {
		i := idx[sp.TransactionID]
		rows[i].Splits = append(rows[i].Splits, sp)
	}
	for i := range rows {
		if len(rows[i].Splits) == 0 {
			continue
		}
		sum := decimal.Zero
		for _, sp := range rows[i].Splits {
			sum = sum.Add(sp.Amount)
		}
		rows[i].SplitsNeedAttention = !sum.Equal(rows[i].Amount)
	}
	return nil
}

func (r *transactionRepo) ReplaceSplits(ctx context.Context, userID, txnID int64, splits []model.TransactionSplit) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.Transaction{}).Where("user_id = ? AND id = ?", userID, txnID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := tx.Where("transaction_id = ? AND user_id = ?", txnID, userID).Delete(&model.TransactionSplit{}).Error; err != nil {
			return err
		}
		if len(splits) == 0 {
			return nil
		}
		for i := range splits {
			splits[i].ID = 0
			splits[i].TransactionID = txnID
			splits[i].UserID = userID
		}
		return tx.Create(&splits).Error
	})
}

// filtered builds the List query's WHERE over transactions.
func (r *transactionRepo) filtered(ctx context.Context, userID int64, f TransactionFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.Transaction{}).Where("user_id = ?", userID)

	if f.AccountID != nil {
		q = q.Where("account_id = ?", *f.AccountID)
	}
	switch {
	case f.Spending:
		cond, args := spendLineCond(userID, f)
		q = q.Where("id IN (SELECT s.id FROM spend_lines s JOIN transactions m ON m.id = s.id WHERE "+cond+")", args...)
	case f.UncategorizedOnly:
		q = q.Where("category_id IS NULL")
	case f.CategoryID != nil:
		q = q.Where("category_id = ?", *f.CategoryID)
	}
	if m := f.CategorizationMethod; m != "" {
		q = q.Where("categorization_method = ?", m)
	}
	if f.Reviewed != nil {
		if *f.Reviewed {
			q = q.Where("reviewed_at IS NOT NULL")
		} else {
			q = q.Where("reviewed_at IS NULL AND kind = ?", model.KindFlow)
		}
	}
	if f.From != nil {
		q = q.Where("transaction_date >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("transaction_date <= ?", *f.To)
	}
	if s := f.Search; s != "" {
		pattern := "%" + s + "%"
		q = q.Where("description ILIKE ? OR merchant_name ILIKE ?", pattern, pattern)
	}
	if f.Tag != "" {
		q = q.Where("tags @> ARRAY[?]::text[]", f.Tag)
	}
	return q
}

func (r *transactionRepo) List(ctx context.Context, userID int64, f TransactionFilter) ([]model.Transaction, int64, error) {
	q := r.filtered(ctx, userID, f)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var out []model.Transaction
	if err := q.
		Order("transaction_date DESC, id DESC").
		Limit(limit).
		Offset(f.Offset).
		Find(&out).Error; err != nil {
		return nil, 0, err
	}
	if err := r.attachSplits(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// SpendTotal sums the spending-lens allocations (positive = spent, refunds
// net against it) of the rows List(f) returns, so a drill-down's total
// equals the Insights / budget figure it came from (#571). Only meaningful
// with f.Spending.
func (r *transactionRepo) SpendTotal(ctx context.Context, userID int64, f TransactionFilter) (decimal.Decimal, error) {
	cond, args := spendLineCond(userID, f)
	ids := r.filtered(ctx, userID, f).Select("id")
	var s string
	err := r.db.WithContext(ctx).Raw(
		"SELECT COALESCE(SUM(-s.amount), 0)::text FROM spend_lines s JOIN transactions m ON m.id = s.id WHERE "+cond+" AND s.id IN (?)",
		append(args, ids)...,
	).Scan(&s).Error
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(s)
}

// TagCount is one of a user's tags and how many live rows carry it (#544).
type TagCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func (r *transactionRepo) ListTags(ctx context.Context, userID int64) ([]TagCount, error) {
	out := []TagCount{}
	err := r.db.WithContext(ctx).Raw(`
		SELECT tag AS name, COUNT(*) AS count
		FROM transactions t, unnest(t.tags) AS tag
		WHERE t.user_id = ? AND t.deleted_at IS NULL
		GROUP BY tag
		ORDER BY lower(tag), tag`, userID).Scan(&out).Error
	return out, err
}

func (r *transactionRepo) Update(ctx context.Context, t *model.Transaction) error {
	res := r.db.WithContext(ctx).
		Where("user_id = ?", t.UserID).
		Save(t)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *transactionRepo) CreateBatch(ctx context.Context, txns []model.Transaction) (int64, error) {
	if len(txns) == 0 {
		return 0, nil
	}
	// ON CONFLICT DO NOTHING on the partial unique index
	// `uq_transactions_user_plaid` (migration 000004 — user-scoped per #63):
	//   (user_id, plaid_transaction_id)
	//   WHERE deleted_at IS NULL AND plaid_transaction_id IS NOT NULL
	// Postgres requires the conflict target's columns AND its partial
	// predicate to match the index exactly, hence TargetWhere — without
	// it we get "no unique or exclusion constraint matching the ON CONFLICT
	// specification".
	//
	// Note: this only catches LIVE duplicates. A soft-deleted row with the
	// same (user_id, plaid_transaction_id) sits outside the partial index,
	// so a naive re-insert WOULD create a second row. The Plaid service's
	// resurrect-on-resurface pass (see plaid.Service.SyncTransactions)
	// handles that case by undeleting the existing row instead of inserting.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "plaid_transaction_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "deleted_at IS NULL AND plaid_transaction_id IS NOT NULL"},
		}},
		DoNothing: true,
	}).Create(&txns)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *transactionRepo) ExistingExternalIDs(ctx context.Context, userID, accountID int64, externalIDs []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if len(externalIDs) == 0 {
		return out, nil
	}
	// GORM's soft-delete scope adds `deleted_at IS NULL`, matching the partial
	// index predicate — so this counts only live duplicates.
	var found []string
	if err := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Where("user_id = ? AND account_id = ? AND external_id IN ?", userID, accountID, externalIDs).
		Pluck("external_id", &found).Error; err != nil {
		return nil, err
	}
	for _, id := range found {
		out[id] = struct{}{}
	}
	return out, nil
}

func (r *transactionRepo) ImportBatch(ctx context.Context, txns []model.Transaction) (int64, error) {
	if len(txns) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_id"}, {Name: "external_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "deleted_at IS NULL AND external_id IS NOT NULL"},
		}},
		DoNothing: true,
	}).Create(&txns)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *transactionRepo) UpdateByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error {
	// Save() needs the row's ID populated to issue an UPDATE rather than
	// an INSERT; callers should be passing the merged row from a prior
	// read so ID is set. Defensive: re-resolve if it isn't.
	if merged.ID == 0 {
		var existing model.Transaction
		if err := r.db.WithContext(ctx).
			Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
			First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		merged.ID = existing.ID
		merged.CreatedAt = existing.CreatedAt
	}
	res := r.db.WithContext(ctx).
		Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
		Save(&merged)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// unlinkPartnersOf clears is_transfer+transfer_pair_id on every live
// transfer partner of the live rows matching cond, so soft-deleting those
// rows never strands a partner flagged as a transfer to nothing. Trade legs
// (is_transfer = FALSE) are untouched. Call inside the delete's transaction.
func unlinkPartnersOf(tx *gorm.DB, userID int64, cond string, args ...any) error {
	sub := tx.Session(&gorm.Session{NewDB: true}).Model(&model.Transaction{}).
		Select("id").Where("user_id = ?", userID).Where(cond, args...)
	return tx.Session(&gorm.Session{NewDB: true}).Model(&model.Transaction{}).
		Where("user_id = ? AND is_transfer = TRUE AND transfer_pair_id IN (?)", userID, sub).
		Updates(map[string]any{"is_transfer": false, "transfer_pair_id": nil}).Error
}

func (r *transactionRepo) SoftDeleteByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := unlinkPartnersOf(tx, userID, "plaid_transaction_id = ?", plaidTxnID); err != nil {
			return err
		}
		res := tx.Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
			Delete(&model.Transaction{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *transactionRepo) FindSoftDeletedByPlaidTransactionIDs(ctx context.Context, userID int64, plaidTxnIDs []string) ([]model.Transaction, error) {
	if len(plaidTxnIDs) == 0 {
		return nil, nil
	}
	var out []model.Transaction
	if err := r.db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND plaid_transaction_id IN ? AND deleted_at IS NOT NULL", userID, plaidTxnIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepo) ResurrectByPlaidTransactionID(ctx context.Context, userID int64, plaidTxnID string, merged model.Transaction) error {
	// Locate the existing row Unscoped so we capture its ID + created_at
	// even though deleted_at is set. Then Save() with DeletedAt zeroed to
	// flip it back to live.
	var existing model.Transaction
	if err := r.db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
		First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	merged.ID = existing.ID
	merged.CreatedAt = existing.CreatedAt
	merged.DeletedAt = gorm.DeletedAt{} // clear soft-delete on the way out
	res := r.db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND plaid_transaction_id = ?", userID, plaidTxnID).
		Save(&merged)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *transactionRepo) ListForCategorizationScope(ctx context.Context, userID int64, scope string, afterID int64, limit int) ([]model.Transaction, error) {
	if limit <= 0 {
		limit = 1000
	}
	q := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Where("user_id = ?", userID)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	switch scope {
	case CategorizationScopeUncategorized:
		q = q.Where("category_id IS NULL")
	case CategorizationScopePlaidDefault:
		q = q.Where("categorization_method = ?", "plaid_default")
	case CategorizationScopeAIEligible:
		q = q.Where("(category_id IS NULL OR categorization_method = ?) AND kind = ?", "plaid_default", model.KindFlow)
	case CategorizationScopeAll, "":
		// no extra predicate
	default:
		return nil, errors.New("unknown categorization scope: " + scope)
	}
	var out []model.Transaction
	if err := q.Order("id ASC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepo) CreateTradePair(ctx context.Context, legA, legB *model.Transaction) error {
	if legA == nil || legB == nil {
		return errors.New("trade pair requires two legs")
	}
	if legA.UserID == 0 || legB.UserID == 0 || legA.UserID != legB.UserID {
		return errors.New("trade pair legs must share a user_id")
	}
	// Wrap both inserts in one DB transaction so a mid-flight failure
	// rolls back the partner row. Use a tx-bound repo for both writes.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(legA).Error; err != nil {
			return err
		}
		if err := tx.Create(legB).Error; err != nil {
			return err
		}
		// Cross-link by ID: legA points at legB, legB points at legA.
		legA.TransferPairID = &legB.ID
		legB.TransferPairID = &legA.ID
		if err := tx.Model(legA).Where("id = ?", legA.ID).
			Update("transfer_pair_id", legB.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(legB).Where("id = ?", legB.ID).
			Update("transfer_pair_id", legA.ID).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *transactionRepo) ListByTransferPairID(ctx context.Context, userID, pairID int64) ([]model.Transaction, error) {
	if pairID <= 0 {
		return nil, ErrNotFound
	}
	var out []model.Transaction
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND (id = ? OR transfer_pair_id = ?)", userID, pairID, pairID).
		Order("id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// lockPosition takes a transaction-scoped advisory lock on the (account,
// asset) position before its ledger is folded (#593). Every position write
// folds the ledger and then upserts the result; without the lock, two
// concurrent writers can each fold before seeing the other's row and the
// slower upsert commits a stale quantity. Under the lock the last writer's
// fold runs after every earlier writer committed, so it sees all their rows.
// It must be its own statement: under READ COMMITTED the fold's snapshot is
// taken when the fold starts, i.e. after the lock is granted. Outside a
// transaction the lock is released when the statement ends, so read-only
// callers only wait out an in-flight writer.
func (r *transactionRepo) lockPosition(ctx context.Context, accountID, assetID int64) error {
	return r.db.WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtextextended(format('position:%s:%s', ?::bigint, ?::bigint), 0))`, accountID, assetID).
		Error
}

func (r *transactionRepo) ListByAccountAndAsset(ctx context.Context, userID, accountID, assetID int64) ([]model.Transaction, error) {
	if err := r.lockPosition(ctx, accountID, assetID); err != nil {
		return nil, err
	}
	var out []model.Transaction
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND account_id = ? AND asset_id = ?", userID, accountID, assetID).
		Order("transaction_date ASC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepo) FoldQuantity(ctx context.Context, userID, accountID, assetID int64) (decimal.Decimal, error) {
	if err := r.lockPosition(ctx, accountID, assetID); err != nil {
		return decimal.Zero, err
	}
	var s string
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(amount), 0)::text
		FROM transactions
		WHERE deleted_at IS NULL
		  AND user_id = ? AND account_id = ? AND asset_id = ?
	`, userID, accountID, assetID).Scan(&s).Error; err != nil {
		return decimal.Zero, err
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, err
	}
	return d, nil
}

// AccountAssetPair identifies one (account, asset) position key. Returned by
// DistinctAccountAssetPairs for the positions rebuild.
type AccountAssetPair struct {
	AccountID int64
	AssetID   int64
}

func (r *transactionRepo) DistinctAccountAssetPairs(ctx context.Context, userID int64) ([]AccountAssetPair, error) {
	var out []AccountAssetPair
	if err := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Distinct("account_id", "asset_id").
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("account_id, asset_id").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepo) FoldByUserAsOf(ctx context.Context, userID int64, asOf time.Time) ([]model.Position, error) {
	type row struct {
		AssetID int64
		Qty     string
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`
		WITH day0 AS (
			SELECT account_id, asset_id, MIN(transaction_date) AS first_date
			FROM transactions
			WHERE deleted_at IS NULL AND user_id = ?
			GROUP BY account_id, asset_id
		)
		SELECT t.asset_id, COALESCE(SUM(t.amount), 0)::text AS qty
		FROM transactions t
		JOIN day0 d ON d.account_id = t.account_id AND d.asset_id = t.asset_id
		WHERE t.deleted_at IS NULL
		  AND t.user_id = ?
		  AND CASE WHEN t.kind = ? THEN d.first_date ELSE t.transaction_date END <= ?
		GROUP BY t.asset_id
		HAVING COALESCE(SUM(t.amount), 0) <> 0
		ORDER BY t.asset_id
	`, userID, userID, model.KindOpeningBalance, asOf).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]model.Position, 0, len(rows))
	for _, r := range rows {
		q, err := decimal.NewFromString(r.Qty)
		if err != nil {
			return nil, err
		}
		out = append(out, model.Position{UserID: userID, AssetID: r.AssetID, Quantity: q})
	}
	return out, nil
}

func (r *transactionRepo) HasReconcilingTxn(ctx context.Context, userID, accountID, assetID int64) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Where("user_id = ? AND account_id = ? AND asset_id = ? AND kind IN ?",
			userID, accountID, assetID, []string{model.KindOpeningBalance, model.KindAdjustment}).
		Limit(1).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *transactionRepo) ListReconciling(ctx context.Context, userID, accountID int64) ([]model.Transaction, error) {
	var out []model.Transaction
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND account_id = ? AND kind IN ?",
			userID, accountID, []string{model.KindOpeningBalance, model.KindAdjustment}).
		Order("transaction_date ASC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *transactionRepo) FoldQuantityBefore(ctx context.Context, userID, accountID, assetID int64, beforeDate time.Time, beforeID int64) (decimal.Decimal, error) {
	var s string
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(amount), 0)::text
		FROM transactions
		WHERE deleted_at IS NULL
		  AND user_id = ? AND account_id = ? AND asset_id = ?
		  AND (transaction_date < ? OR (transaction_date = ? AND id < ?))
	`, userID, accountID, assetID, beforeDate, beforeDate, beforeID).Scan(&s).Error; err != nil {
		return decimal.Zero, err
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, err
	}
	return d, nil
}

func (r *transactionRepo) AcknowledgeAdjustment(ctx context.Context, userID, transactionID int64, note *string) (*model.Transaction, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Where("id = ? AND user_id = ? AND kind = ?", transactionID, userID, model.KindAdjustment).
		Updates(map[string]any{
			"acknowledged_at":   now,
			"acknowledged_note": note,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return r.GetByID(ctx, userID, transactionID)
}

func (r *transactionRepo) ListUnacknowledgedAdjustments(ctx context.Context, userID int64) ([]model.Transaction, error) {
	var out []model.Transaction
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND kind = ? AND acknowledged_at IS NULL", userID, model.KindAdjustment).
		Order("transaction_date ASC, id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// BulkEdit is exactly one bulk action (#567): set or clear the category, or
// set the hidden state.
type BulkEdit struct {
	CategoryID    *int64
	ClearCategory bool
	Hidden        *bool
}

// BulkEditPrior is a row's state before a bulk edit (#567). UpdatedAt is the
// row's updated_at *after* the edit: undo restores only rows still at it.
type BulkEditPrior struct {
	ID                   int64      `json:"id"`
	CategoryID           *int64     `json:"category_id"`
	CategorizationMethod *string    `json:"categorization_method"`
	CategorizationRuleID *int64     `json:"categorization_rule_id"`
	HiddenAt             *time.Time `json:"hidden_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (r *transactionRepo) BulkEdit(ctx context.Context, userID int64, ids []int64, edit BulkEdit) ([]BulkEditPrior, int, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}
	var changed []BulkEditPrior
	skipped := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		type row struct {
			BulkEditPrior
			HasSplits bool
		}
		var rows []row
		if err := tx.Raw(`
			SELECT t.id, t.category_id, t.categorization_method, t.categorization_rule_id, t.hidden_at,
			       EXISTS (SELECT 1 FROM transaction_splits s WHERE s.transaction_id = t.id) AS has_splits
			FROM transactions t
			WHERE t.user_id = ? AND t.id IN ? AND t.deleted_at IS NULL AND t.kind = ?
			ORDER BY t.id
			FOR UPDATE OF t`, userID, ids, model.KindFlow).Scan(&rows).Error; err != nil {
			return err
		}
		var target []int64
		prior := map[int64]BulkEditPrior{}
		for _, rw := range rows {
			change := false
			switch {
			case edit.CategoryID != nil || edit.ClearCategory:
				if rw.HasSplits {
					skipped++
					continue
				}
				if edit.ClearCategory {
					change = rw.CategoryID != nil
				} else {
					change = rw.CategoryID == nil || *rw.CategoryID != *edit.CategoryID ||
						rw.CategorizationMethod == nil || *rw.CategorizationMethod != "manual"
				}
			case edit.Hidden != nil:
				change = (rw.HiddenAt == nil) == *edit.Hidden
			}
			if change {
				target = append(target, rw.ID)
				prior[rw.ID] = rw.BulkEditPrior
			}
		}
		if len(target) == 0 {
			return nil
		}
		var set string
		var args []any
		switch {
		case edit.ClearCategory:
			set = "category_id = NULL, categorization_method = NULL, categorization_rule_id = NULL"
		case edit.CategoryID != nil:
			set = "category_id = ?, categorization_method = 'manual', categorization_rule_id = NULL"
			args = append(args, *edit.CategoryID)
		case *edit.Hidden:
			set = "hidden_at = NOW()"
		default:
			set = "hidden_at = NULL"
		}
		var after []struct {
			ID        int64
			UpdatedAt time.Time
		}
		args = append(args, userID, target)
		if err := tx.Raw(`UPDATE transactions SET `+set+`, updated_at = NOW()
			WHERE user_id = ? AND id IN ?
			RETURNING id, updated_at`, args...).Scan(&after).Error; err != nil {
			return err
		}
		for _, a := range after {
			p := prior[a.ID]
			p.UpdatedAt = a.UpdatedAt
			changed = append(changed, p)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return changed, skipped, nil
}

func (r *transactionRepo) BulkRestore(ctx context.Context, userID int64, prior []BulkEditPrior) (int, error) {
	if len(prior) == 0 {
		return 0, nil
	}
	restored := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A restored rule id must be one of the user's own rules.
		var ruleIDs []int64
		for _, p := range prior {
			if p.CategorizationRuleID != nil {
				ruleIDs = append(ruleIDs, *p.CategorizationRuleID)
			}
		}
		owned := map[int64]bool{}
		if len(ruleIDs) > 0 {
			var ids []int64
			if err := tx.Model(&model.CategorizationRule{}).
				Where("user_id = ? AND id IN ?", userID, ruleIDs).
				Pluck("id", &ids).Error; err != nil {
				return err
			}
			for _, id := range ids {
				owned[id] = true
			}
		}
		for _, p := range prior {
			ruleID := p.CategorizationRuleID
			if ruleID != nil && !owned[*ruleID] {
				ruleID = nil
			}
			res := tx.Exec(`
				UPDATE transactions SET
					category_id            = ?,
					categorization_method  = ?,
					categorization_rule_id = ?,
					hidden_at              = ?,
					updated_at             = NOW()
				WHERE id = ? AND user_id = ? AND deleted_at IS NULL AND updated_at = ?`,
				p.CategoryID, p.CategorizationMethod, ruleID, p.HiddenAt, p.ID, userID, p.UpdatedAt)
			if res.Error != nil {
				return res.Error
			}
			restored += int(res.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return restored, nil
}

func (r *transactionRepo) SetReviewed(ctx context.Context, userID int64, ids []int64, reviewedAt *time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// Updates("reviewed_at", nil) writes NULL; updated_at is deliberately not
	// bumped — review is metadata, not an edit of the transaction itself.
	res := r.db.WithContext(ctx).Model(&model.Transaction{}).
		Where("user_id = ? AND id IN ?", userID, ids).
		UpdateColumn("reviewed_at", reviewedAt)
	return res.RowsAffected, res.Error
}

func (r *transactionRepo) SoftDelete(ctx context.Context, userID, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := unlinkPartnersOf(tx, userID, "id = ?", id); err != nil {
			return err
		}
		res := tx.Where("user_id = ?", userID).Delete(&model.Transaction{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *transactionRepo) ImportJobAssetIDs(ctx context.Context, userID, jobID int64) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Where("user_id = ? AND import_job_id = ?", userID, jobID).
		Distinct().
		Pluck("asset_id", &ids).Error
	return ids, err
}

func (r *transactionRepo) SoftDeleteByImportJob(ctx context.Context, userID, jobID int64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := unlinkPartnersOf(tx, userID, "import_job_id = ?", jobID); err != nil {
			return err
		}
		res := tx.Where("user_id = ? AND import_job_id = ?", userID, jobID).
			Delete(&model.Transaction{})
		n = res.RowsAffected
		return res.Error
	})
	return n, err
}

func (r *transactionRepo) CountLiveByImportJobs(ctx context.Context, userID int64, jobIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(jobIDs))
	if len(jobIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ImportJobID int64
		N           int64
	}
	if err := r.db.WithContext(ctx).
		Model(&model.Transaction{}).
		Select("import_job_id, COUNT(*) AS n").
		Where("user_id = ? AND import_job_id IN ?", userID, jobIDs).
		Group("import_job_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ImportJobID] = r.N
	}
	return out, nil
}
