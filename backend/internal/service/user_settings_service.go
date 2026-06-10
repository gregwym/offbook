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
	ErrInvalidProvider = errors.New("preferred_provider must be 'claude' or 'ollama'")
)

// UserSettingsView is the redacted shape returned over the API. The
// Claude key is NEVER returned to the client — only a boolean flag —
// because exfiltrating a stored bearer key would defeat the encrypt-at-rest
// guarantee.
type UserSettingsView struct {
	UserID            int64   `json:"user_id"`
	ClaudeAPIKeySet   bool    `json:"claude_api_key_set"`
	OllamaBaseURL     *string `json:"ollama_base_url,omitempty"`
	PreferredProvider string  `json:"preferred_provider"`
	PreferredModel    *string `json:"preferred_model,omitempty"`
	// AutoPriceRefresh is the persistent opt-in for the scheduled price
	// refresh (#338 Phase 3, ADR-0014 §3). Default false.
	AutoPriceRefresh bool `json:"auto_price_refresh"`
}

// UpdateUserSettingsInput is a sparse patch — only set fields apply. The
// two Clear* flags exist to disambiguate "leave alone" from "delete":
// sending `claude_api_key=""` is rejected (empty keys are useless);
// sending `clear_claude_api_key=true` removes the stored ciphertext.
type UpdateUserSettingsInput struct {
	ClaudeAPIKey      *string
	ClearClaudeAPIKey bool
	OllamaBaseURL     *string
	ClearOllamaURL    bool
	PreferredProvider *string
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

// GetClaudeAPIKey decrypts and returns the user's Claude key, or
// (empty, nil) when no key is stored. This is the ONLY way callers get
// access to the plaintext; the router-side provider resolver uses it.
//
// The handler MUST NOT call this — it would defeat the redact-on-read
// guarantee. Enforced by code review since Go has no per-method visibility.
func (s *UserSettingsService) GetClaudeAPIKey(ctx context.Context, userID int64) (string, error) {
	row, err := s.fetchOrDefault(ctx, userID)
	if err != nil {
		return "", err
	}
	if len(row.ClaudeAPIKeyEnc) == 0 {
		return "", nil
	}
	plain, err := s.box.Decrypt(row.ClaudeAPIKeyEnc)
	if err != nil {
		return "", fmt.Errorf("user_settings: decrypt claude key: %w", err)
	}
	return string(plain), nil
}

// Update applies the sparse patch and returns the redacted view.
func (s *UserSettingsService) Update(ctx context.Context, userID int64, in UpdateUserSettingsInput) (*UserSettingsView, error) {
	row, err := s.fetchOrDefault(ctx, userID)
	if err != nil {
		return nil, err
	}

	if in.ClearClaudeAPIKey {
		row.ClaudeAPIKeyEnc = nil
	} else if in.ClaudeAPIKey != nil {
		key := strings.TrimSpace(*in.ClaudeAPIKey)
		if key == "" {
			return nil, errors.New("claude_api_key must not be empty (use clear_claude_api_key to delete)")
		}
		enc, err := s.box.Encrypt([]byte(key))
		if err != nil {
			return nil, fmt.Errorf("user_settings: encrypt claude key: %w", err)
		}
		row.ClaudeAPIKeyEnc = enc
	}

	if in.ClearOllamaURL {
		row.OllamaBaseURL = nil
	} else if in.OllamaBaseURL != nil {
		v := strings.TrimSpace(*in.OllamaBaseURL)
		if v == "" {
			row.OllamaBaseURL = nil
		} else {
			row.OllamaBaseURL = &v
		}
	}

	if in.PreferredProvider != nil {
		p := strings.TrimSpace(*in.PreferredProvider)
		if p != "claude" && p != "ollama" {
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

// Resolve is what the AI service / router uses to figure out which
// provider this user wants. Returns the preferred provider, the model
// override (or empty), and the decrypted claude key (or empty).
//
// Callers fall back to env defaults when ClaudeKey is empty AND
// PreferredProvider == "claude" — keeps the local single-tenant deploy
// model working without anyone visiting Settings.
type ResolvedProvider struct {
	Provider      string // "claude" | "ollama"
	Model         string // "" → provider default
	ClaudeKey     string // "" → env fallback applies
	OllamaBaseURL string // "" → env fallback applies
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
	if row.OllamaBaseURL != nil {
		out.OllamaBaseURL = *row.OllamaBaseURL
	}
	if len(row.ClaudeAPIKeyEnc) > 0 {
		plain, err := s.box.Decrypt(row.ClaudeAPIKeyEnc)
		if err != nil {
			return nil, fmt.Errorf("user_settings: decrypt claude key: %w", err)
		}
		out.ClaudeKey = string(plain)
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
		ClaudeAPIKeySet:   len(s.ClaudeAPIKeyEnc) > 0,
		OllamaBaseURL:     s.OllamaBaseURL,
		PreferredProvider: s.PreferredProvider,
		PreferredModel:    s.PreferredModel,
		AutoPriceRefresh:  s.AutoPriceRefresh,
	}
}
