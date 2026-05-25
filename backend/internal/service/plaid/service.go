package plaid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/plaid/plaid-go/v40/plaid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// requestIDPattern strips Plaid `request_id=...` tokens (and JSON `"request_id":"..."`)
// from raw error strings before they're surfaced to users — they're internal-
// trace identifiers, not user-actionable.
var requestIDPattern = regexp.MustCompile(`(?i)(?:[, ]?\brequest_id\b\s*[:=]\s*"?[A-Za-z0-9_-]+"?)`)

// Domain errors. Handlers map these to HTTP status codes.
var (
	// ErrNotConfigured is returned when a Plaid-bound operation is attempted
	// on an instance that has no PLAID_CLIENT_ID. Handlers should 503.
	ErrNotConfigured = errors.New("plaid is not configured on this instance")

	// ErrInvalidPublicToken is returned when /link/exchange is called with an
	// empty or malformed token. Plaid itself enforces the deeper validation;
	// we just block obvious garbage before the round trip.
	ErrInvalidPublicToken = errors.New("public_token is required")

	// ErrItemNotFound is returned when sync-accounts is called against an
	// item_id that doesn't exist or doesn't belong to the session user.
	// Handlers should 404.
	ErrItemNotFound = errors.New("plaid item not found")
)

// SyncAccountsResult summarizes what changed on a discovery run.
// Created + updated together cover every Plaid account on the item.
type SyncAccountsResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// SyncTransactionsResult summarizes a /transactions/sync drain. Inserted
// is the count of rows actually written (i.e., excludes duplicates that
// hit the ON CONFLICT DO NOTHING path). Resurrected counts `added` rows
// whose plaid_transaction_id matched a previously-soft-deleted local row —
// they are undeleted in place rather than inserted as duplicates (see #63).
// Removed is the count of Plaid IDs we were told to forget — for the
// initial pull this is always 0. Failed is the count of individual rows
// that landed in plaid_sync_errors instead of the transactions table —
// the cursor still advances and other rows still commit (see #80 +
// docs/ADR/0011).
type SyncTransactionsResult struct {
	Inserted    int64 `json:"inserted"`
	Resurrected int   `json:"resurrected"`
	Modified    int   `json:"modified"`
	Removed     int   `json:"removed"`
	Failed      int   `json:"failed"`
}

// Service orchestrates the Plaid Link flow: issue link tokens, exchange
// public_tokens for durable access_tokens, and discover accounts behind
// each linked item. access_tokens are encrypted at rest (ADR-0010).
//
// Like every domain service, it scopes by user_id derived from the session
// at the handler layer. Holder names from /identity/get are routed through
// piiSvc into pii_store — never written onto the accounts row directly.
type Service struct {
	client       Client
	box          *crypto.SecretBox
	itemRepo     repository.PlaidItemRepository
	acctRepo     repository.AccountRepository
	txRepo       repository.TransactionRepository
	syncErrRepo  repository.PlaidSyncErrorRepository
	assetRepo    repository.AssetRepository
	positionRepo repository.PositionRepository
	piiSvc       *service.PIIService
	// catMapper resolves Plaid PFCs to local categories at sync time.
	// Nil = no auto-categorization (rows land uncategorized; user picks).
	catMapper *CategoryMapper
	// ruleRepo lets the sync path load the user's categorization rules
	// once per drain and apply them ahead of the Plaid PFC default.
	// Nil = rules feature not wired in this build; sync still works.
	ruleRepo repository.CategorizationRuleRepository
	// db is a deliberate exception to the "services don't see *gorm.DB"
	// guideline. SyncTransactions needs to wrap the per-item drain in a
	// single Postgres transaction so it can use savepoints around each
	// row insert (see #80 / docs/ADR/0011 — partial-success DLQ). Inside
	// the transaction callback we construct fresh tx-bound repo instances
	// so all writes hit the same transaction. SyncAccounts also uses it
	// to wrap the per-account (account + cash position) write pair so a
	// mid-flight failure rolls back both rows together.
	db *gorm.DB
}

// NewService constructs a Service. Pass nil for client/box/repos to
// indicate the instance is not Plaid-enabled — calls then return
// ErrNotConfigured. txRepo, piiSvc, db, and catMapper are only needed
// for the discovery and transaction-sync surfaces; link-only setups can
// pass nil and the affected methods will error out cleanly. catMapper
// may be nil even when Plaid is otherwise configured — transactions
// then land uncategorized.
func NewService(
	client Client,
	box *crypto.SecretBox,
	itemRepo repository.PlaidItemRepository,
	acctRepo repository.AccountRepository,
	txRepo repository.TransactionRepository,
	syncErrRepo repository.PlaidSyncErrorRepository,
	assetRepo repository.AssetRepository,
	positionRepo repository.PositionRepository,
	piiSvc *service.PIIService,
	catMapper *CategoryMapper,
	db *gorm.DB,
) *Service {
	return &Service{
		client:       client,
		box:          box,
		itemRepo:     itemRepo,
		acctRepo:     acctRepo,
		txRepo:       txRepo,
		syncErrRepo:  syncErrRepo,
		assetRepo:    assetRepo,
		positionRepo: positionRepo,
		piiSvc:       piiSvc,
		catMapper:    catMapper,
		db:           db,
	}
}

// WithRuleRepo wires the user-rule repository so SyncTransactions applies
// rules ahead of Plaid's PFC default. Optional; without it the sync still
// works and rows fall back to plaid_default / uncategorized.
func (s *Service) WithRuleRepo(r repository.CategorizationRuleRepository) *Service {
	s.ruleRepo = r
	return s
}

// loadUserRules returns the user's active rules in priority order,
// precompiled. Returns nil when the rule repo isn't wired or the user has
// no rules — callers feed nil straight into MapPlaidTransaction.
func (s *Service) loadUserRules(ctx context.Context, userID int64) ([]categorization.CompiledRule, error) {
	if s == nil || s.ruleRepo == nil {
		return nil, nil
	}
	rules, err := s.ruleRepo.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("plaid: load rules: %w", err)
	}
	return categorization.Compile(rules), nil
}

// Configured reports whether the service has the wiring to actually call
// Plaid. Callers should usually let the methods themselves return
// ErrNotConfigured, but a few surfaces (health, frontend feature flags)
// want a cheap query.
func (s *Service) Configured() bool {
	return s != nil && s.client != nil && s.box != nil && s.itemRepo != nil
}

// CreateLinkToken returns a one-shot link_token the frontend hands to
// Plaid Link. Safe to call repeatedly; tokens expire ~4 hours after issue.
func (s *Service) CreateLinkToken(ctx context.Context, userID int64) (LinkToken, error) {
	if !s.Configured() {
		return LinkToken{}, ErrNotConfigured
	}
	return s.client.CreateLinkToken(ctx, userID)
}

// ListItems returns the user's linked Plaid items (excluding soft-deleted).
// The access_token is never serialized — see model.PlaidItem json tags.
func (s *Service) ListItems(ctx context.Context, userID int64) ([]model.PlaidItem, error) {
	if s == nil || s.itemRepo == nil {
		return nil, ErrNotConfigured
	}
	return s.itemRepo.ListByUser(ctx, userID)
}

// DisconnectItem soft-deletes a plaid_items row by plaid_item_id. Accounts
// previously synced from this item stay visible (and historical) — only the
// upstream connection is severed. Idempotent: deleting an already-deleted
// item returns ErrItemNotFound.
func (s *Service) DisconnectItem(ctx context.Context, userID int64, plaidItemID string) error {
	if s == nil || s.itemRepo == nil {
		return ErrNotConfigured
	}
	item, err := s.itemRepo.GetByPlaidItemID(ctx, userID, plaidItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrItemNotFound
		}
		return fmt.Errorf("plaid: lookup item: %w", err)
	}
	if err := s.itemRepo.SoftDelete(ctx, userID, item.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrItemNotFound
		}
		return fmt.Errorf("plaid: soft-delete item: %w", err)
	}
	return nil
}

// ExchangePublicToken trades a public_token from Plaid Link for a durable
// access_token + item_id, encrypts the access_token, and persists a new
// plaid_items row scoped to userID.
//
// Returns the persisted model.PlaidItem (without the access token, which
// stays inside `AccessTokenEnc`).
func (s *Service) ExchangePublicToken(ctx context.Context, userID int64, publicToken string) (*model.PlaidItem, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	publicToken = strings.TrimSpace(publicToken)
	if publicToken == "" {
		return nil, ErrInvalidPublicToken
	}

	item, err := s.client.ExchangePublicToken(ctx, publicToken)
	if err != nil {
		return nil, err
	}
	if item.AccessToken == "" || item.ItemID == "" {
		// Defensive: Plaid never returns a successful response with these
		// empty, but if the SDK contract ever drifts we want a clear error
		// rather than persisting a useless row.
		return nil, fmt.Errorf("plaid: exchange returned empty token/item_id")
	}

	enc, err := s.box.Encrypt([]byte(item.AccessToken))
	if err != nil {
		return nil, fmt.Errorf("plaid: encrypt access token: %w", err)
	}

	row := &model.PlaidItem{
		UserID:          userID,
		PlaidItemID:     item.ItemID,
		AccessTokenEnc:  enc,
		InstitutionID:   item.InstitutionID,
		InstitutionName: item.InstitutionName,
		Status:          "active",
	}
	if err := s.itemRepo.Create(ctx, row); err != nil {
		return nil, fmt.Errorf("plaid: persist item: %w", err)
	}
	return row, nil
}

// SyncAccounts pulls /accounts/get (plus best-effort /identity/get) for
// the given item and upserts matching accounts rows for the session user.
// Holder names land in pii_store via the injected pii_service; nothing in
// this method writes PII to the accounts row directly.
//
// Idempotency: re-running with no upstream changes produces 0 inserts and
// re-upserts the same pii_store rows (Set is keyed on the unique constraint).
//
// Not wrapped in db.Transaction: the Plaid sync is driven by an external
// source of truth and is naturally re-entrant — a mid-flight failure
// self-heals on the next sync. Wrapping would require threading a *gorm.DB
// through the repo abstraction, which is a broader refactor (filed as
// follow-up if needed).
func (s *Service) SyncAccounts(ctx context.Context, userID int64, plaidItemID string) (SyncAccountsResult, error) {
	if !s.Configured() || s.acctRepo == nil || s.piiSvc == nil {
		return SyncAccountsResult{}, ErrNotConfigured
	}

	item, err := s.itemRepo.GetByPlaidItemID(ctx, userID, plaidItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return SyncAccountsResult{}, ErrItemNotFound
		}
		return SyncAccountsResult{}, fmt.Errorf("plaid: lookup item: %w", err)
	}

	tokenBytes, err := s.box.Decrypt(item.AccessTokenEnc)
	if err != nil {
		return SyncAccountsResult{}, fmt.Errorf("plaid: decrypt access_token: %w", err)
	}
	defer zeroBytes(tokenBytes)

	result, err := s.client.FetchAccounts(ctx, string(tokenBytes))
	if err != nil {
		return SyncAccountsResult{}, err
	}

	// Backfill institution metadata if Plaid surfaces it for the first time.
	// (Often populated on first /accounts/get rather than /item/get/exchange.)
	if item.InstitutionID == nil && result.InstitutionID != nil {
		item.InstitutionID = result.InstitutionID
	}
	if item.InstitutionName == nil && result.InstitutionName != nil {
		item.InstitutionName = result.InstitutionName
	}

	institutionSlug := ""
	if result.InstitutionID != nil {
		institutionSlug = *result.InstitutionID
	}

	var out SyncAccountsResult
	for _, da := range result.Accounts {
		// Resolve the fiat asset for the account's currency before the
		// account write — both the account row and its cash position need it.
		quoteAsset, err := s.assetRepo.EnsureBySymbolKind(ctx, da.Currency, model.AssetKindFiat, da.Currency)
		if err != nil {
			return out, fmt.Errorf("plaid: resolve currency %s: %w", da.Currency, err)
		}

		existing, err := s.acctRepo.FindByPlaidAccountID(ctx, userID, da.PlaidAccountID)
		switch {
		case errors.Is(err, repository.ErrNotFound):
			acct := &model.Account{
				UserID:              userID,
				Name:                da.Name,
				InstitutionSlug:     institutionSlug,
				AccountType:         MapAccountType(da.Type, da.Subtype),
				Currency:            da.Currency,
				PrimaryQuoteAssetID: quoteAsset.ID,
				LastFour:            da.Mask,
				PlaidAccountID:      strPtr(da.PlaidAccountID),
				PlaidItemID:         strPtr(plaidItemID),
				IsActive:            true,
			}
			if err := s.writeAccountAndCashPosition(ctx, acct, quoteAsset.ID, da.Balance); err != nil {
				return out, fmt.Errorf("plaid: create account %s: %w", da.PlaidAccountID, err)
			}
			out.Created++
			if err := s.writeHolderNames(ctx, userID, acct.ID, da.HolderNames); err != nil {
				return out, err
			}
		case err != nil:
			return out, fmt.Errorf("plaid: lookup account %s: %w", da.PlaidAccountID, err)
		default:
			existing.Name = da.Name
			existing.InstitutionSlug = institutionSlug
			existing.AccountType = MapAccountType(da.Type, da.Subtype)
			existing.Currency = da.Currency
			existing.PrimaryQuoteAssetID = quoteAsset.ID
			existing.LastFour = da.Mask
			existing.PlaidItemID = strPtr(plaidItemID)
			existing.IsActive = true
			if err := s.updateAccountAndCashPosition(ctx, existing, quoteAsset.ID, da.Balance); err != nil {
				return out, fmt.Errorf("plaid: update account %d: %w", existing.ID, err)
			}
			out.Updated++
			if err := s.writeHolderNames(ctx, userID, existing.ID, da.HolderNames); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// writeAccountAndCashPosition inserts a new account row and seeds its
// single cash-currency position in one DB transaction. Per ADR-0013, the
// account's value derives from positions × prices — without a position
// row a fresh account would display as $0 even with a non-zero Plaid
// balance.
func (s *Service) writeAccountAndCashPosition(ctx context.Context, acct *model.Account, assetID int64, quantity decimal.Decimal) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		acctRepo := repository.NewAccountRepository(tx)
		if err := acctRepo.Create(ctx, acct); err != nil {
			return err
		}
		pos := &model.Position{
			UserID:    acct.UserID,
			AccountID: acct.ID,
			AssetID:   assetID,
			Quantity:  quantity,
		}
		return repository.NewPositionRepository(tx).Upsert(ctx, pos)
	})
}

// updateAccountAndCashPosition mirrors writeAccountAndCashPosition for the
// upsert path — refreshes the account row and re-bases the cash position
// to whatever Plaid currently reports.
func (s *Service) updateAccountAndCashPosition(ctx context.Context, acct *model.Account, assetID int64, quantity decimal.Decimal) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		acctRepo := repository.NewAccountRepository(tx)
		if err := acctRepo.Update(ctx, acct); err != nil {
			return err
		}
		pos := &model.Position{
			UserID:    acct.UserID,
			AccountID: acct.ID,
			AssetID:   assetID,
			Quantity:  quantity,
		}
		return repository.NewPositionRepository(tx).Upsert(ctx, pos)
	})
}

// writeHolderNames routes Plaid's owner names into pii_store. We coalesce
// multiple names into a single semicolon-delimited string so the storage
// schema (one field per record) stays simple — joint accounts surface as
// "Alice Smith; Bob Smith" rather than overwriting each other.
func (s *Service) writeHolderNames(ctx context.Context, userID, accountID int64, names []string) error {
	cleaned := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			cleaned = append(cleaned, n)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	joined := strings.Join(cleaned, "; ")
	if err := s.piiSvc.SetAccountPII(ctx, userID, accountID, map[string]string{
		"holder_name": joined,
	}); err != nil {
		return fmt.Errorf("plaid: pii holder_name: %w", err)
	}
	return nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

// zeroBytes scrubs a decrypted token buffer after use. Best-effort — Go's
// GC may have already copied — but reduces the window where a heap dump
// would surface the plaintext bearer token.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// safeSyncErrorMessage extracts a user-presentable string from a sync error.
//
// Preference order:
//  1. If the chain contains a Plaid GenericOpenAPIError, surface the
//     structured `error_code` + `display_message` (already polished by Plaid).
//  2. Otherwise fall back to the error string with two redactions: any
//     `request_id=...` token gets dropped, and the result is capped so a
//     pathological wrapped error doesn't blow out the TEXT column or UI pill.
//
// Internal stack frames are already absent — `fmt.Errorf("plaid: ... %w")`
// only contains our own prefixes, which are safe to expose.
func safeSyncErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if pe, convErr := plaid.ToPlaidError(cur); convErr == nil {
			msg := pe.ErrorCode
			if pe.DisplayMessage.IsSet() && pe.DisplayMessage.Get() != nil {
				dm := strings.TrimSpace(*pe.DisplayMessage.Get())
				if dm != "" {
					msg = pe.ErrorCode + ": " + dm
				}
			}
			return capErrorMessage(msg)
		}
	}
	// No structured Plaid error → redact request_id from the wrapped text.
	return capErrorMessage(requestIDPattern.ReplaceAllString(err.Error(), ""))
}

// capErrorMessage trims and caps to keep the TEXT column + UI pill sane.
func capErrorMessage(s string) string {
	s = strings.TrimSpace(s)
	const max = 500
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// SyncTransactions runs /transactions/sync for the given item. Works
// identically for the first-time historical pull (empty cursor) and for
// incremental refreshes (persisted cursor) — the same Plaid endpoint
// covers both via the cursor handshake.
//
// Two-phase per-item drain (#62 + #80):
//   - Phase 1: page through Plaid until exhaustion, accumulating in memory.
//     No DB writes — a Plaid failure can't leave the DB half-updated.
//   - Phase 2: one db.Transaction. Each `added`/`modified`/`removed` row is
//     processed inside its own SAVEPOINT so a single bad row (decimal
//     overflow, bad date, mapping error) goes to plaid_sync_errors instead
//     of rolling back the whole batch. The cursor + last_synced_at advance
//     once at the end regardless of how many rows DLQ'd.
//
// Sync status terminus:
//   - All rows ok → 'ok'
//   - At least one DLQ'd row → 'ok_with_errors' (post-commit write)
//   - Transactional / Plaid failure → 'error' (recorded by the defer)
//
// See docs/ADR/0011 for the rationale on advancing the cursor across
// partial failures.
func (s *Service) SyncTransactions(ctx context.Context, userID int64, plaidItemID string) (result SyncTransactionsResult, retErr error) {
	if !s.Configured() || s.acctRepo == nil || s.txRepo == nil || s.syncErrRepo == nil || s.db == nil {
		return SyncTransactionsResult{}, ErrNotConfigured
	}

	item, err := s.itemRepo.GetByPlaidItemID(ctx, userID, plaidItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return SyncTransactionsResult{}, ErrItemNotFound
		}
		return SyncTransactionsResult{}, fmt.Errorf("plaid: lookup item: %w", err)
	}

	// Per-sync lifecycle: flip to 'syncing' up front. On success, UpdateCursor
	// (inside the DB transaction below) flips to 'ok' and clears the error.
	// On error/panic, the defer flips to 'error' with a user-safe message.
	// Use a background ctx for the status writes so a request cancel doesn't
	// leave the row stuck in 'syncing'.
	statusCtx := context.WithoutCancel(ctx)
	if err := s.itemRepo.UpdateSyncStatus(statusCtx, userID, item.ID, "syncing", nil); err != nil {
		return SyncTransactionsResult{}, fmt.Errorf("plaid: mark syncing: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("panic during sync: %v", r)
			_ = s.itemRepo.UpdateSyncStatus(statusCtx, userID, item.ID, "error", &msg)
			panic(r)
		}
		if retErr != nil {
			msg := safeSyncErrorMessage(retErr)
			_ = s.itemRepo.UpdateSyncStatus(statusCtx, userID, item.ID, "error", &msg)
		}
	}()

	tokenBytes, err := s.box.Decrypt(item.AccessTokenEnc)
	if err != nil {
		return SyncTransactionsResult{}, fmt.Errorf("plaid: decrypt access_token: %w", err)
	}
	defer zeroBytes(tokenBytes)
	accessToken := string(tokenBytes)

	cursor := ""
	if item.Cursor != nil {
		cursor = *item.Cursor
	}

	// Phase 1: drain every page from Plaid into memory. No DB writes
	// happen here so a Plaid failure can't leave the DB half-updated.
	var added, modified []PlaidTransaction
	var removed []string
	for {
		page, err := s.client.SyncTransactions(ctx, accessToken, cursor)
		if err != nil {
			return SyncTransactionsResult{}, err
		}
		added = append(added, page.Added...)
		modified = append(modified, page.Modified...)
		removed = append(removed, page.Removed...)
		cursor = page.NextCursor
		if !page.HasMore {
			break
		}
	}

	// Load the user's categorization rules once for the whole drain.
	// Doing this before the DB transaction means a rule-repo failure
	// fails the sync cleanly without leaving the txn half-open.
	userRules, err := s.loadUserRules(ctx, userID)
	if err != nil {
		return SyncTransactionsResult{}, err
	}

	// Phase 2: one transaction for all writes. Tx-bound repos so every
	// statement hits the same Postgres transaction. Per-row savepoints
	// isolate individual failures — see SAVEPOINT helper below.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		itemRepo := repository.NewPlaidItemRepository(tx)
		acctRepo := repository.NewAccountRepository(tx)
		syncErrRepo := repository.NewPlaidSyncErrorRepository(tx)
		accountIDCache := map[string]accountRef{}
		spIdx := 0

		// withSavepoint wraps fn in a Postgres SAVEPOINT so a row-level
		// failure (constraint violation, type error) rolls back only that
		// row's work, not the whole batch. After ROLLBACK TO SAVEPOINT
		// the outer transaction is still alive — subsequent statements
		// (including the DLQ insert) execute normally.
		withSavepoint := func(fn func() error) error {
			spIdx++
			name := fmt.Sprintf("plaid_row_%d", spIdx)
			if err := tx.SavePoint(name).Error; err != nil {
				return fmt.Errorf("plaid: savepoint %s: %w", name, err)
			}
			if err := fn(); err != nil {
				if rErr := tx.RollbackTo(name).Error; rErr != nil {
					return fmt.Errorf("plaid: rollback to %s: %w (orig: %v)", name, rErr, err)
				}
				return err
			}
			return nil
		}

		// recordDLQ writes a plaid_sync_errors row. Runs OUTSIDE the
		// just-rolled-back savepoint so it can't itself be reverted by a
		// later RollbackTo. Stays inside the outer transaction so the
		// cursor advance and DLQ rows commit atomically.
		recordDLQ := func(pt PlaidTransaction, code string, cause error) error {
			raw, mErr := json.Marshal(pt)
			if mErr != nil {
				raw = json.RawMessage(`{}`)
			}
			var ptxIDPtr *string
			if pt.PlaidTransactionID != "" {
				v := pt.PlaidTransactionID
				ptxIDPtr = &v
			}
			row := &model.PlaidSyncError{
				UserID:             userID,
				PlaidItemID:        item.ID,
				PlaidTransactionID: ptxIDPtr,
				RawPayload:         raw,
				ErrorCode:          code,
				ErrorMessage:       capErrorMessage(cause.Error()),
				OccurredAt:         time.Now().UTC(),
			}
			if err := syncErrRepo.Create(ctx, row); err != nil {
				return fmt.Errorf("plaid: write DLQ row: %w", err)
			}
			result.Failed++
			return nil
		}

		// classifyRowError categorizes a per-row failure so the UI can
		// group by error_code. account-resolution and mapping errors are
		// surfaced separately from raw DB errors because they tend to
		// point at different fixes (run sync-accounts vs. file a bug).
		classifyRowError := func(err error) string {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "no local account for plaid_account_id"):
				return model.PlaidSyncErrorCodeMapping
			case strings.Contains(msg, "parse date"), strings.Contains(msg, "parse authorized_date"):
				return model.PlaidSyncErrorCodeValidation
			case strings.Contains(msg, "numeric field overflow"), strings.Contains(msg, "value out of range"):
				return model.PlaidSyncErrorCodeDecimalOverflow
			case strings.Contains(msg, "transaction_id empty"), strings.Contains(msg, "account_id empty"):
				return model.PlaidSyncErrorCodeValidation
			default:
				return model.PlaidSyncErrorCodeDB
			}
		}

		// Inserts. Two cases per `added`:
		//   1. plaid_transaction_id matches a soft-deleted local row →
		//      resurrect (clear deleted_at + overlay via MergePlaidUpdate
		//      so user-edited notes/category survive the round-trip).
		//   2. otherwise → single-row insert with ON CONFLICT DO NOTHING.
		// Both paths run inside a savepoint so one bad row just DLQs.
		if len(added) > 0 {
			plaidIDs := make([]string, 0, len(added))
			addedByPlaidID := make(map[string]PlaidTransaction, len(added))
			for _, pt := range added {
				plaidIDs = append(plaidIDs, pt.PlaidTransactionID)
				addedByPlaidID[pt.PlaidTransactionID] = pt
			}

			deleted, err := txRepo.FindSoftDeletedByPlaidTransactionIDs(ctx, userID, plaidIDs)
			if err != nil {
				return fmt.Errorf("plaid: lookup soft-deleted for resurrect: %w", err)
			}
			resurrectedIDs := make(map[string]struct{}, len(deleted))
			for _, existing := range deleted {
				if existing.PlaidTransactionID == nil {
					continue
				}
				ptxID := *existing.PlaidTransactionID
				incoming, ok := addedByPlaidID[ptxID]
				if !ok {
					continue
				}
				perRowErr := withSavepoint(func() error {
					ref, err := resolveAccount(ctx, acctRepo, userID, incoming.PlaidAccountID, accountIDCache)
					if err != nil {
						return err
					}
					merged, err := MergePlaidUpdate(existing, incoming, ref.ID, ref.AssetID, s.catMapper, userRules)
					if err != nil {
						return err
					}
					return txRepo.ResurrectByPlaidTransactionID(ctx, userID, ptxID, merged)
				})
				if perRowErr != nil {
					if err := recordDLQ(incoming, classifyRowError(perRowErr), perRowErr); err != nil {
						return err
					}
					// Mark as "handled" so the insert pass below doesn't
					// try again on the same row.
					resurrectedIDs[ptxID] = struct{}{}
					continue
				}
				resurrectedIDs[ptxID] = struct{}{}
				result.Resurrected++
			}

			for _, pt := range added {
				if _, done := resurrectedIDs[pt.PlaidTransactionID]; done {
					continue
				}
				perRowErr := withSavepoint(func() error {
					ref, err := resolveAccount(ctx, acctRepo, userID, pt.PlaidAccountID, accountIDCache)
					if err != nil {
						return err
					}
					row, err := MapPlaidTransaction(pt, userID, ref.ID, ref.AssetID, s.catMapper, userRules)
					if err != nil {
						return err
					}
					n, err := txRepo.CreateBatch(ctx, []model.Transaction{row})
					if err != nil {
						return err
					}
					result.Inserted += n
					return nil
				})
				if perRowErr != nil {
					if err := recordDLQ(pt, classifyRowError(perRowErr), perRowErr); err != nil {
						return err
					}
				}
			}
		}

		// Modifications: merge with the existing row so user-edited
		// fields survive. Wrap each in a savepoint so a single bad
		// `modified` doesn't poison the rest of the batch.
		for _, pt := range modified {
			var insertedAsNew bool
			perRowErr := withSavepoint(func() error {
				ref, err := resolveAccount(ctx, acctRepo, userID, pt.PlaidAccountID, accountIDCache)
				if err != nil {
					return err
				}
				var existing model.Transaction
				err = tx.WithContext(ctx).
					Where("user_id = ? AND plaid_transaction_id = ?", userID, pt.PlaidTransactionID).
					First(&existing).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					// Plaid sent a `modified` for something we never
					// recorded. Treat as a fresh insert so we don't lose it.
					row, mapErr := MapPlaidTransaction(pt, userID, ref.ID, ref.AssetID, s.catMapper, userRules)
					if mapErr != nil {
						return mapErr
					}
					if _, batchErr := txRepo.CreateBatch(ctx, []model.Transaction{row}); batchErr != nil {
						return batchErr
					}
					insertedAsNew = true
					return nil
				}
				if err != nil {
					return err
				}
				merged, err := MergePlaidUpdate(existing, pt, ref.ID, ref.AssetID, s.catMapper, userRules)
				if err != nil {
					return err
				}
				return txRepo.UpdateByPlaidTransactionID(ctx, userID, pt.PlaidTransactionID, merged)
			})
			if perRowErr != nil {
				if err := recordDLQ(pt, classifyRowError(perRowErr), perRowErr); err != nil {
					return err
				}
				continue
			}
			if insertedAsNew {
				result.Inserted++
			} else {
				result.Modified++
			}
		}

		// Removals: soft-delete. ErrNotFound is benign (already deleted
		// or never seen). A real DB error here is rare and almost always
		// systemic — abort rather than DLQ a string ID with no payload.
		for _, ptxID := range removed {
			err := txRepo.SoftDeleteByPlaidTransactionID(ctx, userID, ptxID)
			switch {
			case errors.Is(err, repository.ErrNotFound):
				// Already gone — count it anyway so the response number
				// matches what Plaid asked us to forget.
				result.Removed++
			case err != nil:
				return fmt.Errorf("plaid: soft-delete removed: %w", err)
			default:
				result.Removed++
			}
		}

		// Cursor advance — final write inside the same transaction. Always
		// happens, even when result.Failed > 0: the whole point of the DLQ
		// is to let good rows commit and let the cursor move past the
		// poison row instead of replaying the failure forever (#80).
		// UpdateCursor sets status='ok' + clears last_sync_error; if any
		// rows DLQ'd we flip to 'ok_with_errors' AFTER commit (below).
		if err := itemRepo.UpdateCursor(ctx, userID, item.ID, cursor, time.Now().UTC()); err != nil {
			return fmt.Errorf("plaid: persist cursor: %w", err)
		}
		return nil
	})
	if err != nil {
		return SyncTransactionsResult{}, err
	}

	// Post-commit status fix-up: if any rows DLQ'd, mark the item
	// 'ok_with_errors' with a summary message. Done outside the tx so a
	// failure here is non-fatal — the cursor and DLQ rows already
	// committed; the worst case is a stale 'ok' status that the next sync
	// will overwrite. statusCtx is detached so a request cancel doesn't
	// leave it inconsistent.
	if result.Failed > 0 {
		msg := fmt.Sprintf("%d row(s) failed to import; see Linked Institutions", result.Failed)
		_ = s.itemRepo.UpdateSyncStatus(statusCtx, userID, item.ID, "ok_with_errors", &msg)
	}
	return result, nil
}

// accountRef bundles the two values transaction mapping needs from a Plaid
// account_id lookup: the local accounts.id and the account's
// primary_quote_asset_id (which becomes the transaction's asset_id).
type accountRef struct {
	ID      int64
	AssetID int64
}

// resolveAccount maps a Plaid account_id to (local id, primary quote asset),
// memoizing in a per-call cache. Returns an error if no local account
// exists — the caller should run /sync-accounts first. Operates against
// the supplied AccountRepository so transactional callers can pass a
// tx-bound instance.
func resolveAccount(
	ctx context.Context,
	repo repository.AccountRepository,
	userID int64,
	plaidAcctID string,
	cache map[string]accountRef,
) (accountRef, error) {
	if ref, ok := cache[plaidAcctID]; ok {
		return ref, nil
	}
	a, err := repo.FindByPlaidAccountID(ctx, userID, plaidAcctID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return accountRef{}, fmt.Errorf("plaid: no local account for plaid_account_id=%s (run sync-accounts first)", plaidAcctID)
		}
		return accountRef{}, fmt.Errorf("plaid: lookup account %s: %w", plaidAcctID, err)
	}
	ref := accountRef{ID: a.ID, AssetID: a.PrimaryQuoteAssetID}
	cache[plaidAcctID] = ref
	return ref, nil
}
