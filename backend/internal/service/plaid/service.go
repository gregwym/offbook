package plaid

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
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
)

// Service orchestrates the Plaid Link flow: issue link tokens and exchange
// the resulting public_token for a durable access_token that we persist
// encrypted (ADR-0010).
//
// Like every domain service, it scopes by user_id derived from the session
// at the handler layer.
type Service struct {
	client Client
	box    *crypto.SecretBox
	repo   repository.PlaidItemRepository
}

// NewService constructs a Service. Pass a nil client/box to indicate the
// instance is not Plaid-enabled — calls then return ErrNotConfigured.
func NewService(client Client, box *crypto.SecretBox, repo repository.PlaidItemRepository) *Service {
	return &Service{client: client, box: box, repo: repo}
}

// Configured reports whether the service has the wiring to actually call
// Plaid. Callers should usually let the methods themselves return
// ErrNotConfigured, but a few surfaces (health, frontend feature flags)
// want a cheap query.
func (s *Service) Configured() bool {
	return s != nil && s.client != nil && s.box != nil && s.repo != nil
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
	if err := s.repo.Create(ctx, row); err != nil {
		return nil, fmt.Errorf("plaid: persist item: %w", err)
	}
	return row, nil
}
