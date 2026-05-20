// Package plaid wraps the Plaid Go SDK behind a small interface so the
// service layer can be tested without an SDK dependency and so future
// alternate clients (e.g. for replay) drop in cleanly.
//
// The bearer access_token is stored encrypted at rest (ADR-0010). Nothing
// in this package should ever return it to a caller other than the service
// that immediately encrypts and persists it.
package plaid

import (
	"context"
	"fmt"
	"time"

	"github.com/plaid/plaid-go/v40/plaid"
	"github.com/shopspring/decimal"
)

// Client is the minimum Plaid surface this project depends on. New methods
// land here as new milestones come online (accounts, transactions sync).
type Client interface {
	// CreateLinkToken returns a short-lived link_token for the frontend to
	// hand to Plaid Link. userID is opaque to Plaid but Plaid requires *some*
	// stable per-user identifier for fraud signal.
	CreateLinkToken(ctx context.Context, userID int64) (LinkToken, error)

	// ExchangePublicToken trades the public_token Plaid Link returns to the
	// frontend for the durable access_token + item_id that future API calls
	// use. The access_token is bearer-equivalent — never log it.
	ExchangePublicToken(ctx context.Context, publicToken string) (Item, error)

	// FetchAccounts pulls /accounts/get and best-effort /identity/get for
	// the linked item. Identity is optional: if Plaid returns an error
	// (typically PRODUCTS_NOT_SUPPORTED on sandbox banks without identity),
	// HolderNames are empty on every account but the call still succeeds.
	FetchAccounts(ctx context.Context, accessToken string) (AccountsResult, error)

	// SyncTransactions wraps one /transactions/sync call. An empty cursor
	// triggers the historical pull; a non-empty cursor delivers only what
	// changed since that cursor. Callers paginate by re-calling with
	// page.NextCursor while page.HasMore is true.
	SyncTransactions(ctx context.Context, accessToken, cursor string) (SyncTransactionsPage, error)
}

// SyncTransactionsPage is one page of /transactions/sync output. The cursor
// is opaque — never inspect it, just persist and feed back on the next call.
type SyncTransactionsPage struct {
	Added      []PlaidTransaction
	Modified   []PlaidTransaction
	Removed    []string // plaid_transaction_id values to soft-delete
	NextCursor string
	HasMore    bool
}

// PlaidTransaction is the slim view of one Plaid transaction. The
// amount-sign convention here matches Plaid's: positive = money OUT of
// the account. transaction_mapping.go flips this for project storage.
type PlaidTransaction struct {
	PlaidTransactionID string
	PlaidAccountID     string
	Amount             decimal.Decimal // Plaid sign convention
	Currency           string
	Name               string  // Plaid `name` (raw description)
	MerchantName       *string // Plaid `merchant_name` (may be nil)
	Date               string  // "YYYY-MM-DD"
	AuthorizedDate     *string // optional "YYYY-MM-DD"
	Pending            bool
	// Personal finance category — Plaid's hierarchical taxonomy.
	// Strings (not enums) so we never need a Go release to add a new PFC.
	// May be "" when Plaid hasn't classified a transaction (rare).
	PFCPrimary  string
	PFCDetailed string
}

// AccountsResult bundles the institution descriptor and the account list
// returned from /accounts/get + /identity/get.
type AccountsResult struct {
	InstitutionID   *string
	InstitutionName *string
	Accounts        []DiscoveredAccount
}

// DiscoveredAccount is the slim view of one Plaid account that the
// discovery flow needs. Holder names land here (from /identity/get) and
// are routed into pii_store — never persisted on the accounts row.
type DiscoveredAccount struct {
	PlaidAccountID string
	Name           string  // official_name preferred, then name — never a holder
	Mask           *string // last 4 (Plaid `mask`)
	Type           string  // raw Plaid type ("depository", "credit", ...)
	Subtype        string  // raw Plaid subtype ("checking", "credit card", ...)
	Currency       string  // ISO; defaults to USD if Plaid omits it
	Balance        decimal.Decimal
	HolderNames    []string // from /identity/get; empty when identity unavailable
}

// LinkToken is the result of /link/token/create.
type LinkToken struct {
	Token      string    // pass to Plaid Link
	Expiration time.Time // ~4 hours from issue
}

// Item is the result of /item/public_token/exchange.
type Item struct {
	AccessToken     string  // bearer — must be encrypted at rest
	ItemID          string  // durable Plaid Item identifier
	InstitutionID   *string // populated when the institution is known
	InstitutionName *string // resolved later via /institutions/get
}

// SDKClient is the production implementation. Wraps a configured plaid.APIClient.
type SDKClient struct {
	api         *plaid.APIClient
	products    []plaid.Products
	countryCode []plaid.CountryCode
	clientName  string
	language    string
}

// Config is the minimum input needed to construct an SDKClient.
type Config struct {
	ClientID string
	Secret   string
	// Env is one of "sandbox", "development", "production". Anything else is
	// treated as a literal URL override (useful for httptest in unit tests).
	Env string
}

// NewSDKClient builds an SDKClient with the conventional defaults
// (transactions product, US country code, English). M3+ surfaces will add
// configurability as they need it.
func NewSDKClient(cfg Config) (*SDKClient, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("plaid: ClientID is required")
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("plaid: Secret is required")
	}
	apiCfg := plaid.NewConfiguration()
	apiCfg.AddDefaultHeader("PLAID-CLIENT-ID", cfg.ClientID)
	apiCfg.AddDefaultHeader("PLAID-SECRET", cfg.Secret)
	apiCfg.UseEnvironment(resolveEnv(cfg.Env))

	return &SDKClient{
		api: plaid.NewAPIClient(apiCfg),
		// Request transactions + identity so /identity/get works after link.
		// Auth (account/routing numbers) intentionally omitted — not every
		// sandbox institution supports it and we don't need it yet.
		products:    []plaid.Products{plaid.PRODUCTS_TRANSACTIONS, plaid.PRODUCTS_IDENTITY},
		countryCode: []plaid.CountryCode{plaid.COUNTRYCODE_US},
		clientName:  "Offbook",
		language:    "en",
	}, nil
}

// resolveEnv maps our friendly env names to Plaid SDK environments. An
// unknown value falls through as a URL override so tests can point the SDK
// at an httptest.Server.
func resolveEnv(env string) plaid.Environment {
	switch env {
	case "sandbox", "":
		return plaid.Sandbox
	case "production":
		return plaid.Production
	default:
		return plaid.Environment(env) // URL override (httptest, custom hosts)
	}
}

func (c *SDKClient) CreateLinkToken(ctx context.Context, userID int64) (LinkToken, error) {
	req := plaid.NewLinkTokenCreateRequest(c.clientName, c.language, c.countryCode)
	req.SetUser(*plaid.NewLinkTokenCreateRequestUser(fmt.Sprintf("user-%d", userID)))
	req.SetProducts(c.products)

	resp, _, err := c.api.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*req).Execute()
	if err != nil {
		return LinkToken{}, fmt.Errorf("plaid: link/token/create: %w", err)
	}
	return LinkToken{
		Token:      resp.GetLinkToken(),
		Expiration: resp.GetExpiration(),
	}, nil
}

func (c *SDKClient) ExchangePublicToken(ctx context.Context, publicToken string) (Item, error) {
	req := plaid.NewItemPublicTokenExchangeRequest(publicToken)
	resp, _, err := c.api.PlaidApi.ItemPublicTokenExchange(ctx).ItemPublicTokenExchangeRequest(*req).Execute()
	if err != nil {
		return Item{}, fmt.Errorf("plaid: item/public_token/exchange: %w", err)
	}
	return Item{
		AccessToken: resp.GetAccessToken(),
		ItemID:      resp.GetItemId(),
		// /item/public_token/exchange does not return institution details;
		// they're populated on the first FetchAccounts call instead.
	}, nil
}

func (c *SDKClient) FetchAccounts(ctx context.Context, accessToken string) (AccountsResult, error) {
	accReq := plaid.NewAccountsGetRequest(accessToken)
	accResp, _, err := c.api.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*accReq).Execute()
	if err != nil {
		return AccountsResult{}, fmt.Errorf("plaid: accounts/get: %w", err)
	}

	// Identity is best-effort. If the institution / product set doesn't
	// support it, swallow the error and fall through with no holder names.
	holderNamesByAcct := map[string][]string{}
	idReq := plaid.NewIdentityGetRequest(accessToken)
	if idResp, _, idErr := c.api.PlaidApi.IdentityGet(ctx).IdentityGetRequest(*idReq).Execute(); idErr == nil {
		for _, ai := range idResp.GetAccounts() {
			id := ai.GetAccountId()
			var names []string
			for _, owner := range ai.GetOwners() {
				names = append(names, owner.GetNames()...)
			}
			if len(names) > 0 {
				holderNamesByAcct[id] = names
			}
		}
	}

	item := accResp.GetItem()
	out := AccountsResult{
		Accounts: make([]DiscoveredAccount, 0, len(accResp.GetAccounts())),
	}
	if id, ok := item.GetInstitutionIdOk(); ok && id != nil && *id != "" {
		v := *id
		out.InstitutionID = &v
	}
	if name, ok := item.GetInstitutionNameOk(); ok && name != nil && *name != "" {
		v := *name
		out.InstitutionName = &v
	}

	for _, a := range accResp.GetAccounts() {
		name := a.GetOfficialName()
		if name == "" {
			name = a.GetName()
		}
		var mask *string
		if m, ok := a.GetMaskOk(); ok && m != nil && *m != "" {
			v := *m
			mask = &v
		}
		bal := a.GetBalances()
		// Plaid returns balances as float64. Round-trip through string so
		// shopspring/decimal preserves the JSON-side value exactly.
		balDec := decimal.NewFromFloat(bal.GetCurrent())
		currency := bal.GetIsoCurrencyCode()
		if currency == "" {
			currency = "USD"
		}
		out.Accounts = append(out.Accounts, DiscoveredAccount{
			PlaidAccountID: a.GetAccountId(),
			Name:           name,
			Mask:           mask,
			Type:           string(a.GetType()),
			Subtype:        string(a.GetSubtype()),
			Currency:       currency,
			Balance:        balDec,
			HolderNames:    holderNamesByAcct[a.GetAccountId()],
		})
	}
	return out, nil
}

func (c *SDKClient) SyncTransactions(ctx context.Context, accessToken, cursor string) (SyncTransactionsPage, error) {
	req := plaid.NewTransactionsSyncRequest(accessToken)
	if cursor != "" {
		req.SetCursor(cursor)
	}
	// Opt in to personal_finance_category on every row. Without this flag
	// Plaid omits PFC for new items, so MapPlaidCategory("","") returns
	// (0, false) and the row stays uncategorized — #181.
	opts := plaid.NewTransactionsSyncRequestOptions()
	opts.SetIncludePersonalFinanceCategory(true)
	req.SetOptions(*opts)
	resp, _, err := c.api.PlaidApi.TransactionsSync(ctx).TransactionsSyncRequest(*req).Execute()
	if err != nil {
		return SyncTransactionsPage{}, fmt.Errorf("plaid: transactions/sync: %w", err)
	}

	page := SyncTransactionsPage{
		Added:      make([]PlaidTransaction, 0, len(resp.GetAdded())),
		Modified:   make([]PlaidTransaction, 0, len(resp.GetModified())),
		Removed:    make([]string, 0, len(resp.GetRemoved())),
		NextCursor: resp.GetNextCursor(),
		HasMore:    resp.GetHasMore(),
	}
	for _, t := range resp.GetAdded() {
		page.Added = append(page.Added, convertPlaidTransaction(t))
	}
	for _, t := range resp.GetModified() {
		page.Modified = append(page.Modified, convertPlaidTransaction(t))
	}
	for _, r := range resp.GetRemoved() {
		if id, ok := r.GetTransactionIdOk(); ok && id != nil && *id != "" {
			page.Removed = append(page.Removed, *id)
		}
	}
	return page, nil
}

// convertPlaidTransaction reshapes the SDK type into our slim struct.
// Amount stays in Plaid's sign convention (positive = outflow); the
// service-side mapper flips for project storage.
func convertPlaidTransaction(t plaid.Transaction) PlaidTransaction {
	out := PlaidTransaction{
		PlaidTransactionID: t.GetTransactionId(),
		PlaidAccountID:     t.GetAccountId(),
		Amount:             decimal.NewFromFloat(t.GetAmount()),
		Currency:           t.GetIsoCurrencyCode(),
		Name:               t.GetName(),
		Date:               t.GetDate(),
		Pending:            t.GetPending(),
	}
	if out.Currency == "" {
		out.Currency = "USD"
	}
	if mn, ok := t.GetMerchantNameOk(); ok && mn != nil && *mn != "" {
		v := *mn
		out.MerchantName = &v
	}
	if ad, ok := t.GetAuthorizedDateOk(); ok && ad != nil && *ad != "" {
		v := *ad
		out.AuthorizedDate = &v
	}
	if pfc, ok := t.GetPersonalFinanceCategoryOk(); ok && pfc != nil {
		out.PFCPrimary = pfc.GetPrimary()
		out.PFCDetailed = pfc.GetDetailed()
	}
	return out
}
