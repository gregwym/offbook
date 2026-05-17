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
		api:         plaid.NewAPIClient(apiCfg),
		products:    []plaid.Products{plaid.PRODUCTS_TRANSACTIONS},
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
		// they're fetched separately via /item/get in M3#60. Leaving these
		// nil here keeps the responsibilities cleanly split.
	}, nil
}
