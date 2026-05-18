package plaid

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

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
// initial pull this is always 0.
type SyncTransactionsResult struct {
	Inserted    int64 `json:"inserted"`
	Resurrected int   `json:"resurrected"`
	Modified    int   `json:"modified"`
	Removed     int   `json:"removed"`
}

// Service orchestrates the Plaid Link flow: issue link tokens, exchange
// public_tokens for durable access_tokens, and discover accounts behind
// each linked item. access_tokens are encrypted at rest (ADR-0010).
//
// Like every domain service, it scopes by user_id derived from the session
// at the handler layer. Holder names from /identity/get are routed through
// piiSvc into pii_store — never written onto the accounts row directly.
type Service struct {
	client   Client
	box      *crypto.SecretBox
	itemRepo repository.PlaidItemRepository
	acctRepo repository.AccountRepository
	txRepo   repository.TransactionRepository
	piiSvc   *service.PIIService
	// catMapper resolves Plaid PFCs to local categories at sync time.
	// Nil = no auto-categorization (rows land uncategorized; user picks).
	catMapper *CategoryMapper
	// db is a deliberate exception to the "services don't see *gorm.DB"
	// guideline. SyncTransactions needs a single atomic wrap across
	// transactions + plaid_items updates per ADR-N/A (per-item sync is
	// all-or-nothing — see issue #62 spec) and the repo abstraction
	// doesn't currently expose a unit-of-work surface. Inside the
	// transaction callback we construct fresh tx-bound repo instances
	// so all writes hit the same transaction.
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
	piiSvc *service.PIIService,
	catMapper *CategoryMapper,
	db *gorm.DB,
) *Service {
	return &Service{
		client:    client,
		box:       box,
		itemRepo:  itemRepo,
		acctRepo:  acctRepo,
		txRepo:    txRepo,
		piiSvc:    piiSvc,
		catMapper: catMapper,
		db:        db,
	}
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
		existing, err := s.acctRepo.FindByPlaidAccountID(ctx, userID, da.PlaidAccountID)
		switch {
		case errors.Is(err, repository.ErrNotFound):
			acct := &model.Account{
				UserID:          userID,
				Name:            da.Name,
				InstitutionSlug: institutionSlug,
				AccountType:     MapAccountType(da.Type, da.Subtype),
				Currency:        da.Currency,
				Balance:         da.Balance,
				LastFour:        da.Mask,
				PlaidAccountID:  strPtr(da.PlaidAccountID),
				PlaidItemID:     strPtr(plaidItemID),
				IsActive:        true,
			}
			if err := s.acctRepo.Create(ctx, acct); err != nil {
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
			existing.Balance = da.Balance
			existing.LastFour = da.Mask
			existing.PlaidItemID = strPtr(plaidItemID)
			existing.IsActive = true
			if err := s.acctRepo.Update(ctx, existing); err != nil {
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

// SyncTransactions runs /transactions/sync for the given item. Works
// identically for the first-time historical pull (empty cursor) and for
// incremental refreshes (persisted cursor) — the same Plaid endpoint
// covers both via the cursor handshake.
//
// Per #62, the entire per-item sync is wrapped in one db.Transaction:
//   - all pages are drained from Plaid first (no DB writes)
//   - then `added` rows are inserted, `modified` rows merged into existing
//     rows (preserving user-edited category_id / notes), and `removed`
//     rows soft-deleted
//   - cursor + last_synced_at advance ONLY if all of the above succeed
//
// If anything in the DB phase fails, the cursor stays put and next run
// re-fetches the same delta — safe because added uses ON CONFLICT DO
// NOTHING and modified/removed are idempotent on plaid_transaction_id.
func (s *Service) SyncTransactions(ctx context.Context, userID int64, plaidItemID string) (SyncTransactionsResult, error) {
	if !s.Configured() || s.acctRepo == nil || s.txRepo == nil || s.db == nil {
		return SyncTransactionsResult{}, ErrNotConfigured
	}

	item, err := s.itemRepo.GetByPlaidItemID(ctx, userID, plaidItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return SyncTransactionsResult{}, ErrItemNotFound
		}
		return SyncTransactionsResult{}, fmt.Errorf("plaid: lookup item: %w", err)
	}

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

	var result SyncTransactionsResult
	// Phase 2: one transaction for all writes. Tx-bound repos so every
	// statement hits the same Postgres transaction; rollback on any error
	// reverts the entire sync including the cursor advance.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := repository.NewTransactionRepository(tx)
		itemRepo := repository.NewPlaidItemRepository(tx)
		acctRepo := repository.NewAccountRepository(tx)
		accountIDCache := map[string]int64{}

		// Inserts. Two cases per `added`:
		//   1. plaid_transaction_id matches a soft-deleted local row →
		//      resurrect (clear deleted_at + overlay via MergePlaidUpdate
		//      so user-edited notes/category survive the round-trip).
		//   2. otherwise → batch insert with ON CONFLICT DO NOTHING.
		// The resurrect pass is what gives #63 its "re-sync of a previously
		// deleted Plaid row restores in place" guarantee — without it, the
		// partial unique index (which excludes deleted rows) would silently
		// let a duplicate land.
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
				localID, err := resolveAccountID(ctx, acctRepo, userID, incoming.PlaidAccountID, accountIDCache)
				if err != nil {
					return err
				}
				merged, err := MergePlaidUpdate(existing, incoming, localID, s.catMapper)
				if err != nil {
					return err
				}
				if err := txRepo.ResurrectByPlaidTransactionID(ctx, userID, ptxID, merged); err != nil {
					return fmt.Errorf("plaid: resurrect %s: %w", ptxID, err)
				}
				resurrectedIDs[ptxID] = struct{}{}
				result.Resurrected++
			}

			batch := make([]model.Transaction, 0, len(added)-len(resurrectedIDs))
			for _, pt := range added {
				if _, done := resurrectedIDs[pt.PlaidTransactionID]; done {
					continue
				}
				localID, err := resolveAccountID(ctx, acctRepo, userID, pt.PlaidAccountID, accountIDCache)
				if err != nil {
					return err
				}
				row, err := MapPlaidTransaction(pt, userID, localID, s.catMapper)
				if err != nil {
					return err
				}
				batch = append(batch, row)
			}
			if len(batch) > 0 {
				n, err := txRepo.CreateBatch(ctx, batch)
				if err != nil {
					return fmt.Errorf("plaid: insert batch: %w", err)
				}
				result.Inserted = n
			}
		}

		// Modifications: merge with the existing row so user-edited
		// fields survive.
		for _, pt := range modified {
			localID, err := resolveAccountID(ctx, acctRepo, userID, pt.PlaidAccountID, accountIDCache)
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
				row, mapErr := MapPlaidTransaction(pt, userID, localID, s.catMapper)
				if mapErr != nil {
					return mapErr
				}
				if _, batchErr := txRepo.CreateBatch(ctx, []model.Transaction{row}); batchErr != nil {
					return fmt.Errorf("plaid: insert modified-as-new: %w", batchErr)
				}
				result.Inserted++
				continue
			}
			if err != nil {
				return fmt.Errorf("plaid: read existing for modify: %w", err)
			}
			merged, err := MergePlaidUpdate(existing, pt, localID, s.catMapper)
			if err != nil {
				return err
			}
			if err := txRepo.UpdateByPlaidTransactionID(ctx, userID, pt.PlaidTransactionID, merged); err != nil {
				return fmt.Errorf("plaid: update modified: %w", err)
			}
			result.Modified++
		}

		// Removals: soft-delete. ErrNotFound is benign (already deleted
		// or never seen) and shouldn't roll back the transaction.
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

		// Cursor advance — final write inside the same transaction.
		if err := itemRepo.UpdateCursor(ctx, userID, item.ID, cursor, time.Now().UTC()); err != nil {
			return fmt.Errorf("plaid: persist cursor: %w", err)
		}
		return nil
	})
	if err != nil {
		return SyncTransactionsResult{}, err
	}
	return result, nil
}

// resolveAccountID maps a Plaid account_id to the local accounts.id,
// memoizing in a per-call cache. Returns an error if no local account
// exists — the caller should run /sync-accounts first. Operates against
// the supplied AccountRepository so transactional callers can pass a
// tx-bound instance.
func resolveAccountID(
	ctx context.Context,
	repo repository.AccountRepository,
	userID int64,
	plaidAcctID string,
	cache map[string]int64,
) (int64, error) {
	if id, ok := cache[plaidAcctID]; ok {
		return id, nil
	}
	a, err := repo.FindByPlaidAccountID(ctx, userID, plaidAcctID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return 0, fmt.Errorf("plaid: no local account for plaid_account_id=%s (run sync-accounts first)", plaidAcctID)
		}
		return 0, fmt.Errorf("plaid: lookup account %s: %w", plaidAcctID, err)
	}
	cache[plaidAcctID] = a.ID
	return a.ID, nil
}
