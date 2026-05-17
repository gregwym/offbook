package plaid

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	piiSvc   *service.PIIService
}

// NewService constructs a Service. Pass nil for client/box/repos to
// indicate the instance is not Plaid-enabled — calls then return
// ErrNotConfigured. The PIIService is only needed for FetchAccounts; for
// link-only setups it can be nil and SyncAccounts will error out cleanly.
func NewService(
	client Client,
	box *crypto.SecretBox,
	itemRepo repository.PlaidItemRepository,
	acctRepo repository.AccountRepository,
	piiSvc *service.PIIService,
) *Service {
	return &Service{
		client:   client,
		box:      box,
		itemRepo: itemRepo,
		acctRepo: acctRepo,
		piiSvc:   piiSvc,
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
