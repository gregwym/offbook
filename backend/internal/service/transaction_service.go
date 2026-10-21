package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// Domain errors for transactions.
var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrInvalidCategory     = errors.New("invalid category")
	ErrInvalidAmount       = errors.New("amount must not be zero")
	ErrInvalidSource       = errors.New("invalid source")
	ErrMissingDate         = errors.New("transaction_date is required")
	ErrInvalidSplits       = errors.New("invalid splits")
	ErrSplitAmountLocked   = errors.New("transaction is split; clear its splits before changing the amount")
)

// validSources mirrors the transactions.source CHECK constraint (migration
// 000023). 'system' is reconciliation-only (written by ReconcilePosition, never
// via Create) so it is intentionally absent here.
var validSources = map[string]struct{}{
	"manual": {},
	"plaid":  {},
	"csv":    {},
	"pdf":    {},
	"photo":  {},
}

// CreateTransactionInput is the validated, decoded create payload.
// Source defaults to "manual" if empty. The transaction's asset is derived
// from the parent account's primary_quote_asset — cash-transaction-only in
// this PR; trade-pair entry (#238) will let callers override the asset.
type CreateTransactionInput struct {
	AccountID       int64
	CategoryID      *int64
	Amount          decimal.Decimal
	Description     *string
	MerchantName    *string
	TransactionDate time.Time
	PostedDate      *time.Time
	Source          string
	Notes           *string
}

// UpdateTransactionInput is a sparse patch. Per ADR-0013, asset_id on an
// existing transaction is not user-mutable — to "change the currency" of a
// transaction, delete it and re-enter under a different account.
type UpdateTransactionInput struct {
	CategoryID      *int64
	ClearCategory   bool
	Amount          *decimal.Decimal
	Description     *string
	MerchantName    *string
	TransactionDate *time.Time
	PostedDate      *time.Time
	Notes           *string
	// Reviewed sets (true) or clears (false) review state; nil leaves it alone.
	// Review is independent of category edits (#424): changing a category does
	// not implicitly mark a row reviewed, and vice versa.
	Reviewed *bool
	// Hidden hides (true) or unhides (false) a flow row from spending, budgets
	// and merchant reports (#465); nil leaves it alone. Ledger is unaffected.
	Hidden *bool
	// Tags replaces the row's tags (#544); nil leaves them alone, an empty
	// slice clears them. Metadata only.
	Tags *[]string
}

// TransactionService owns business rules for transactions.
// All read paths take the session user_id; cross-user reads are impossible.
type TransactionService struct {
	repo         repository.TransactionRepository
	accountRepo  repository.AccountRepository
	categoryRepo repository.CategoryRepository
	// ruleRepo lets Create apply the user's categorization rules when the
	// caller didn't supply a CategoryID. Optional — when nil, manual rows
	// land uncategorized (the M2-era behavior).
	ruleRepo repository.CategorizationRuleRepository
	// jobRepo backs the AI import staging store (ADR-0019 §7). Optional — when
	// nil, ExtractAndStage/CommitJob return ErrImportStagingUnavailable.
	jobRepo repository.IngestionJobRepository
	// db + assetRepo enable statement position/closing-balance reconciliation
	// (#375); see WithStatementPositions. Optional.
	db        *gorm.DB
	assetRepo repository.AssetRepository
}

func NewTransactionService(
	repo repository.TransactionRepository,
	accountRepo repository.AccountRepository,
	categoryRepo repository.CategoryRepository,
) *TransactionService {
	return &TransactionService{repo: repo, accountRepo: accountRepo, categoryRepo: categoryRepo}
}

// WithRuleRepo wires the user-rule repository so Create applies rules to
// uncategorized inserts. Returns the receiver for one-liner construction.
func (s *TransactionService) WithRuleRepo(r repository.CategorizationRuleRepository) *TransactionService {
	s.ruleRepo = r
	return s
}

// WithJobRepo wires the ingestion_jobs repository, enabling AI extract/stage/
// commit (ADR-0019 §7). Returns the receiver for one-liner construction.
func (s *TransactionService) WithJobRepo(r repository.IngestionJobRepository) *TransactionService {
	s.jobRepo = r
	return s
}

func (s *TransactionService) Create(ctx context.Context, userID int64, in CreateTransactionInput) (*model.Transaction, error) {
	src := strings.TrimSpace(in.Source)
	if src == "" {
		src = "manual"
	}
	if _, ok := validSources[src]; !ok {
		return nil, ErrInvalidSource
	}
	if in.Amount.IsZero() {
		return nil, ErrInvalidAmount
	}
	if in.TransactionDate.IsZero() {
		return nil, ErrMissingDate
	}
	// Fetch the account against the session user — this rejects transactions
	// targeting someone else's account and gives us the asset_id for the row.
	account, err := s.fetchOwnedAccount(ctx, userID, in.AccountID)
	if err != nil {
		return nil, err
	}
	if in.CategoryID != nil {
		if err := s.assertCategoryExists(ctx, *in.CategoryID); err != nil {
			return nil, err
		}
	}

	t := &model.Transaction{
		UserID:          userID,
		AccountID:       in.AccountID,
		AssetID:         account.PrimaryQuoteAssetID,
		CategoryID:      in.CategoryID,
		Amount:          in.Amount,
		Description:     trimPtr(in.Description),
		MerchantName:    trimPtr(in.MerchantName),
		TransactionDate: in.TransactionDate,
		PostedDate:      in.PostedDate,
		Source:          src,
		Notes:           in.Notes,
	}
	if src == "manual" {
		// A row the user typed in themselves has, by definition, been seen.
		now := time.Now().UTC()
		t.ReviewedAt = &now
	}
	if in.CategoryID != nil {
		method := "manual"
		t.CategorizationMethod = &method
	} else if s.ruleRepo != nil {
		// No user-picked category — try the user's rules. We load and
		// compile per-Create because manual-entry volume is low (a few
		// rows per day per user); the cost is negligible and avoids
		// stale-cache bugs when the user has just edited a rule.
		rules, err := s.ruleRepo.List(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("load rules: %w", err)
		}
		categorization.Apply(t, categorization.Compile(rules))
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}
	if err := s.refreshPosition(ctx, userID, t.AccountID, t.AssetID); err != nil {
		return nil, err
	}
	return t, nil
}

// refreshPosition re-materializes the (account, asset) position from the
// ledger fold (ADR-0017: positions.quantity == Σ non-deleted transactions) so
// account balance and net worth follow manual edits instead of staying at
// 0.00 until a statement or sync lands (#446). No-op without db wiring (unit
// tests that don't exercise valuation).
func (s *TransactionService) refreshPosition(ctx context.Context, userID, accountID, assetID int64) error {
	if s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		qty, err := repository.NewTransactionRepository(tx).FoldQuantity(ctx, userID, accountID, assetID)
		if err != nil {
			return fmt.Errorf("fold position: %w", err)
		}
		if err := repository.NewPositionRepository(tx).Upsert(ctx, &model.Position{
			UserID: userID, AccountID: accountID, AssetID: assetID, Quantity: qty,
		}); err != nil {
			return fmt.Errorf("refresh position: %w", err)
		}
		return nil
	})
}

func (s *TransactionService) List(ctx context.Context, userID int64, f repository.TransactionFilter) ([]model.Transaction, int64, error) {
	return s.repo.List(ctx, userID, f)
}

func (s *TransactionService) Get(ctx context.Context, userID, id int64) (*model.Transaction, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	return t, nil
}

func (s *TransactionService) Update(ctx context.Context, userID, id int64, in UpdateTransactionInput) (*model.Transaction, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}

	switch {
	case in.ClearCategory:
		t.CategoryID = nil
		t.CategorizationMethod = nil
	case in.CategoryID != nil:
		if err := s.assertCategoryExists(ctx, *in.CategoryID); err != nil {
			return nil, err
		}
		t.CategoryID = in.CategoryID
		method := "manual"
		t.CategorizationMethod = &method
	}

	if in.Amount != nil {
		if in.Amount.IsZero() {
			return nil, ErrInvalidAmount
		}
		if len(t.Splits) > 0 && !in.Amount.Equal(t.Amount) {
			return nil, ErrSplitAmountLocked
		}
		t.Amount = *in.Amount
	}
	if in.Description != nil {
		t.Description = trimPtr(in.Description)
	}
	if in.MerchantName != nil {
		t.MerchantName = trimPtr(in.MerchantName)
	}
	if in.TransactionDate != nil {
		if in.TransactionDate.IsZero() {
			return nil, ErrMissingDate
		}
		t.TransactionDate = *in.TransactionDate
	}
	if in.PostedDate != nil {
		if in.PostedDate.IsZero() {
			t.PostedDate = nil
		} else {
			pd := *in.PostedDate
			t.PostedDate = &pd
		}
	}
	if in.Notes != nil {
		t.Notes = in.Notes
	}
	if in.Tags != nil {
		tags, err := NormalizeTags(*in.Tags)
		if err != nil {
			return nil, err
		}
		if t.Tags, err = s.canonicalTags(ctx, userID, tags); err != nil {
			return nil, err
		}
	}
	if in.Reviewed != nil {
		if *in.Reviewed {
			if t.ReviewedAt == nil {
				now := time.Now().UTC()
				t.ReviewedAt = &now
			}
		} else {
			t.ReviewedAt = nil
		}
	}
	if in.Hidden != nil {
		if *in.Hidden {
			if t.HiddenAt == nil {
				now := time.Now().UTC()
				t.HiddenAt = &now
			}
		} else {
			t.HiddenAt = nil
		}
	}

	if err := s.repo.Update(ctx, t); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("update transaction: %w", err)
	}
	if in.Amount != nil {
		if err := s.refreshPosition(ctx, userID, t.AccountID, t.AssetID); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// SplitInput is one requested split line (#438).
type SplitInput struct {
	CategoryID *int64
	Amount     decimal.Decimal
	Note       *string
}

// MaxSplits caps lines per transaction.
const MaxSplits = 20

// ReplaceSplits sets the category splits of a transaction (#438). Rules: flow,
// non-transfer rows only; at least 2 lines (empty clears); every line non-zero
// with the parent's sign; lines sum EXACTLY to the parent amount (decimal).
// The parent's amount/ledger effect is never touched.
func (s *TransactionService) ReplaceSplits(ctx context.Context, userID, id int64, in []SplitInput) (*model.Transaction, error) {
	t, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	var splits []model.TransactionSplit
	if len(in) > 0 {
		if t.Kind != model.KindFlow || t.IsTransfer {
			return nil, fmt.Errorf("%w: only non-transfer flow transactions can be split", ErrInvalidSplits)
		}
		if len(in) < 2 || len(in) > MaxSplits {
			return nil, fmt.Errorf("%w: need between 2 and %d lines", ErrInvalidSplits, MaxSplits)
		}
		sum := decimal.Zero
		for _, l := range in {
			if l.Amount.IsZero() || l.Amount.Sign() != t.Amount.Sign() {
				return nil, fmt.Errorf("%w: each line must be non-zero with the same sign as the transaction", ErrInvalidSplits)
			}
			if l.CategoryID != nil {
				if err := s.assertCategoryExists(ctx, *l.CategoryID); err != nil {
					return nil, err
				}
			}
			sum = sum.Add(l.Amount)
			splits = append(splits, model.TransactionSplit{CategoryID: l.CategoryID, Amount: l.Amount, Note: trimPtr(l.Note)})
		}
		if !sum.Equal(t.Amount) {
			return nil, fmt.Errorf("%w: lines sum to %s but the transaction is %s", ErrInvalidSplits, sum.String(), t.Amount.String())
		}
	}
	if err := s.repo.ReplaceSplits(ctx, userID, id, splits); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("replace splits: %w", err)
	}
	return s.Get(ctx, userID, id)
}

// MaxReviewBatch caps one bulk review call.
const MaxReviewBatch = 500

// ErrInvalidReviewBatch is returned for an empty or oversized id list.
var ErrInvalidReviewBatch = errors.New("ids must contain 1 to 500 transaction ids")

// SetReviewed marks the given transactions reviewed (reviewed=true) or
// unreviewed (false) in one statement and returns how many rows changed.
// Idempotent; ids not owned by the user are ignored. Already-reviewed rows
// keep their original reviewed_at on re-review.
func (s *TransactionService) SetReviewed(ctx context.Context, userID int64, ids []int64, reviewed bool) (int64, error) {
	if len(ids) == 0 || len(ids) > MaxReviewBatch {
		return 0, ErrInvalidReviewBatch
	}
	var at *time.Time
	if reviewed {
		now := time.Now().UTC()
		at = &now
	}
	n, err := s.repo.SetReviewed(ctx, userID, ids, at)
	if err != nil {
		return 0, fmt.Errorf("set reviewed: %w", err)
	}
	return n, nil
}

// ErrInvalidBulkEdit: a bulk edit must name exactly one action (#567).
var ErrInvalidBulkEdit = errors.New("set exactly one of category_id, clear_category or hidden")

// BulkEditInput is one bulk action over selected rows (#567).
type BulkEditInput struct {
	CategoryID    *int64
	ClearCategory bool
	Hidden        *bool
}

// BulkEditResult reports a bulk edit. Undo holds the prior state of every
// changed row; posting it back to BulkUndo reverts exactly this edit.
type BulkEditResult struct {
	Updated      int                        `json:"updated"`
	SkippedSplit int                        `json:"skipped_split"`
	Undo         []repository.BulkEditPrior `json:"undo"`
}

// BulkEdit sets or clears the category of, or hides/unhides, the selected
// flow rows in one DB transaction (#567). A category pick is a manual
// categorization, exactly like the single-row edit; split parents are
// skipped. Amounts, dates, review state and the ledger are never touched.
// Ids the user doesn't own are ignored.
func (s *TransactionService) BulkEdit(ctx context.Context, userID int64, ids []int64, in BulkEditInput) (*BulkEditResult, error) {
	if len(ids) == 0 || len(ids) > MaxReviewBatch {
		return nil, ErrInvalidReviewBatch
	}
	actions := 0
	if in.CategoryID != nil {
		actions++
	}
	if in.ClearCategory {
		actions++
	}
	if in.Hidden != nil {
		actions++
	}
	if actions != 1 {
		return nil, ErrInvalidBulkEdit
	}
	if in.CategoryID != nil {
		if err := s.assertCategoryExists(ctx, *in.CategoryID); err != nil {
			return nil, err
		}
	}
	prior, skipped, err := s.repo.BulkEdit(ctx, userID, ids, repository.BulkEdit{
		CategoryID: in.CategoryID, ClearCategory: in.ClearCategory, Hidden: in.Hidden,
	})
	if err != nil {
		return nil, fmt.Errorf("bulk edit: %w", err)
	}
	if prior == nil {
		prior = []repository.BulkEditPrior{}
	}
	return &BulkEditResult{Updated: len(prior), SkippedSplit: skipped, Undo: prior}, nil
}

// BulkUndo reverts a bulk edit from its Undo list (#567). A row changed since
// the edit (a manual edit, a sync merge, a rule run) is kept as it is.
// Returns how many rows were restored.
func (s *TransactionService) BulkUndo(ctx context.Context, userID int64, prior []repository.BulkEditPrior) (int, error) {
	if len(prior) == 0 || len(prior) > MaxReviewBatch {
		return 0, ErrInvalidReviewBatch
	}
	for _, p := range prior {
		if p.CategorizationMethod != nil && !validCategorizationMethod[*p.CategorizationMethod] {
			return 0, ErrInvalidBulkEdit
		}
	}
	n, err := s.repo.BulkRestore(ctx, userID, prior)
	if err != nil {
		return 0, fmt.Errorf("bulk undo: %w", err)
	}
	return n, nil
}

var validCategorizationMethod = map[string]bool{"manual": true, "rule": true, "ai": true, "plaid_default": true}

func (s *TransactionService) SoftDelete(ctx context.Context, userID, id int64) error {
	t, gerr := s.repo.GetByID(ctx, userID, id)
	if err := s.repo.SoftDelete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTransactionNotFound
		}
		return fmt.Errorf("soft delete transaction: %w", err)
	}
	if gerr == nil {
		return s.refreshPosition(ctx, userID, t.AccountID, t.AssetID)
	}
	return nil
}

func (s *TransactionService) fetchOwnedAccount(ctx context.Context, userID, accountID int64) (*model.Account, error) {
	a, err := s.accountRepo.GetByID(ctx, userID, accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	return a, nil
}

func (s *TransactionService) assertCategoryExists(ctx context.Context, id int64) error {
	if _, err := s.categoryRepo.GetByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrInvalidCategory
		}
		return err
	}
	return nil
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}
