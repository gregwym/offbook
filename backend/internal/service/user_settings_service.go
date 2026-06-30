package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
)

var (
	ErrInvalidProvider = errors.New("preferred_provider must be 'claude', 'ollama', or 'openai'")
)

// UserSettingsView is the redacted shape returned over the API. The API
// token is NEVER returned to the client — only a boolean flag — because
// exfiltrating a stored bearer token would defeat the encrypt-at-rest
// guarantee.
type UserSettingsView struct {
	UserID int64 `json:"user_id"`
	// PreferredProvider is the API protocol (claude | ollama | openai).
	PreferredProvider string  `json:"preferred_provider"`
	APIEndpoint       *string `json:"api_endpoint,omitempty"`
	APITokenSet       bool    `json:"api_token_set"`
	PreferredModel    *string `json:"preferred_model,omitempty"`
	// AutoPriceRefresh is the persistent opt-in for the scheduled price
	// refresh (#338 Phase 3, ADR-0014 §3). Default false.
	AutoPriceRefresh bool `json:"auto_price_refresh"`
}

// UpdateUserSettingsInput is a sparse patch — only set fields apply. The
// Clear* flags disambiguate "leave alone" from "delete": sending
// `api_token=""` is rejected (empty tokens are useless); sending
// `clear_api_token=true` removes the stored ciphertext.
type UpdateUserSettingsInput struct {
	PreferredProvider *string
	APIEndpoint       *string
	ClearAPIEndpoint  bool
	APIToken          *string
	ClearAPIToken     bool
	PreferredModel    *string
	ClearModel        bool
	AutoPriceRefresh  *bool
}

// UserSettingsService owns user_settings CRUD + Claude-key encryption.
// It is the ONLY service with access to the Claude SecretBox — handlers
// and the AI service must go through here.
type UserSettingsService struct {
	repo repository.UserSettingsRepository
	box  *crypto.SecretBox
}

// NewUserSettingsService wires the service. The SecretBox is constructed
// once at boot from a SESSION_SECRET-derived key; see router.go for the
// derivation.
func NewUserSettingsService(repo repository.UserSettingsRepository, box *crypto.SecretBox) *UserSettingsService {
	return &UserSettingsService{repo: repo, box: box}
}

// Get returns the redacted view for an existing row, or a default row
// when the user has never saved settings. Auto-create-on-read keeps the
// API surface idempotent: GET is safe even on fresh accounts.
func (s *UserSettingsService) Get(ctx context.Context, userID int64) (*UserSettingsView, error) {
	row, err := s.fetchOrDefault(ctx, userID)
	if err != nil {
		return nil, err
	}
	return view(row), nil
}

// Update applies the sparse patch and returns the redacted view.
func (s *UserSettingsService) Update(ctx context.Context, userID int64, in UpdateUserSettingsInput) (*UserSettingsView, error) {
	row, err := s.fetchOrDefault(ctx, userID)
	if err != nil {
		return nil, err
	}

	if in.ClearAPIToken {
		row.APITokenEnc = nil
	} else if in.APIToken != nil {
		key := strings.TrimSpace(*in.APIToken)
		if key == "" {
			return nil, errors.New("api_token must not be empty (use clear_api_token to delete)")
		}
		enc, err := s.box.Encrypt([]byte(key))
		if err != nil {
			return nil, fmt.Errorf("user_settings: encrypt api token: %w", err)
		}
		row.APITokenEnc = enc
	}

	if in.ClearAPIEndpoint {
		row.APIEndpoint = nil
	} else if in.APIEndpoint != nil {
		v := strings.TrimSpace(*in.APIEndpoint)
		if v == "" {
			row.APIEndpoint = nil
		} else {
			row.APIEndpoint = &v
		}
	}

	if in.PreferredProvider != nil {
		p := strings.TrimSpace(*in.PreferredProvider)
		if p != "claude" && p != "ollama" && p != "openai" {
			return nil, ErrInvalidProvider
		}
		row.PreferredProvider = p
	}

	if in.ClearModel {
		row.PreferredModel = nil
	} else if in.PreferredModel != nil {
		m := strings.TrimSpace(*in.PreferredModel)
		if m == "" {
			row.PreferredModel = nil
		} else {
			row.PreferredModel = &m
		}
	}

	if in.AutoPriceRefresh != nil {
		row.AutoPriceRefresh = *in.AutoPriceRefresh
	}

	if err := s.repo.Upsert(ctx, row); err != nil {
		return nil, fmt.Errorf("user_settings: upsert: %w", err)
	}
	return view(row), nil
}

// Resolve is what the AI service / router uses to figure out which provider
// this user wants: the protocol, the model override (or empty), the endpoint
// (or empty → provider default), and the decrypted token (or empty).
//
// Callers fall back to env defaults when Token is empty AND Provider ==
// "claude" — keeps the local single-tenant deploy working without anyone
// visiting Settings.
type ResolvedProvider struct {
	Provider string // "claude" | "ollama" | "openai"
	Model    string // "" → provider default
	Endpoint string // "" → provider default
	Token    string // "" → env fallback (claude) or omitted bearer (openai)
}

func (s *UserSettingsService) Resolve(ctx context.Context, userID int64) (*ResolvedProvider, error) {
	row, err := s.fetchOrDefault(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &ResolvedProvider{
		Provider: row.PreferredProvider,
	}
	if row.PreferredModel != nil {
		out.Model = *row.PreferredModel
	}
	if row.APIEndpoint != nil {
		out.Endpoint = *row.APIEndpoint
	}
	if len(row.APITokenEnc) > 0 {
		plain, err := s.box.Decrypt(row.APITokenEnc)
		if err != nil {
			return nil, fmt.Errorf("user_settings: decrypt api token: %w", err)
		}
		out.Token = string(plain)
	}
	return out, nil
}

func (s *UserSettingsService) fetchOrDefault(ctx context.Context, userID int64) (*model.UserSettings, error) {
	row, err := s.repo.Get(ctx, userID)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("user_settings: get: %w", err)
	}
	row = &model.UserSettings{UserID: userID, PreferredProvider: "claude"}
	if err := s.repo.Upsert(ctx, row); err != nil {
		return nil, fmt.Errorf("user_settings: create: %w", err)
	}
	return row, nil
}

func view(s *model.UserSettings) *UserSettingsView {
	return &UserSettingsView{
		UserID:            s.UserID,
		PreferredProvider: s.PreferredProvider,
		APIEndpoint:       s.APIEndpoint,
		APITokenSet:       len(s.APITokenEnc) > 0,
		PreferredModel:    s.PreferredModel,
		AutoPriceRefresh:  s.AutoPriceRefresh,
	}
}
