package service_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func newUserSettingsSvc(t *testing.T) (*service.UserSettingsService, int64) {
	t.Helper()
	g := openTestDB(t)
	userID := seedTestUser(t, g)

	sum := sha256.Sum256([]byte("test-session-secret"))
	box, err := crypto.NewSecretBox(sum[:])
	if err != nil {
		t.Fatalf("NewSecretBox: %v", err)
	}
	svc := service.NewUserSettingsService(repository.NewUserSettingsRepository(g), box)

	t.Cleanup(func() {
		// SoftDelete on users cascades into user_settings via FK; the user
		// cleanup registered by seedTestUser handles it. Belt-and-braces:
		// hard-delete settings explicitly so re-runs are clean.
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.UserSettings{})
	})
	return svc, userID
}

// TestUserSettings_Get_Default returns a default row on first read.
func TestUserSettings_Get_Default(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	v, err := svc.Get(context.Background(), userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.ClaudeAPIKeySet {
		t.Errorf("ClaudeAPIKeySet = true on fresh user, want false")
	}
	if v.PreferredProvider != "claude" {
		t.Errorf("PreferredProvider = %q, want 'claude'", v.PreferredProvider)
	}
}

// TestUserSettings_Update_StoresAndHidesClaudeKey verifies the encrypt-at-
// rest + redact-on-read contract: the plaintext is recoverable via
// Resolve(), but Get() never returns the key — only ClaudeAPIKeySet=true.
func TestUserSettings_Update_StoresAndHidesClaudeKey(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()

	key := "sk-ant-test-12345"
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{
		ClaudeAPIKey: &key,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !v.ClaudeAPIKeySet {
		t.Errorf("ClaudeAPIKeySet = false after setting, want true")
	}

	// Resolve must be able to recover the plaintext (router-side provider
	// resolver depends on it).
	resolved, err := svc.Resolve(ctx, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.ClaudeKey != key {
		t.Errorf("Resolve.ClaudeKey = %q, want %q", resolved.ClaudeKey, key)
	}
}

// TestUserSettings_Update_ClearClaudeKey removes the stored ciphertext.
func TestUserSettings_Update_ClearClaudeKey(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()
	k := "sk-test"
	if _, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{ClaudeAPIKey: &k}); err != nil {
		t.Fatalf("Update set: %v", err)
	}
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{ClearClaudeAPIKey: true})
	if err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if v.ClaudeAPIKeySet {
		t.Errorf("ClaudeAPIKeySet = true after clear, want false")
	}
	resolved, _ := svc.Resolve(ctx, userID)
	if resolved.ClaudeKey != "" {
		t.Errorf("Resolve.ClaudeKey = %q after clear, want empty", resolved.ClaudeKey)
	}
}

// TestUserSettings_Update_EmptyKeyRejected — empty plaintext is a
// user-error (probably a stripped paste); we reject rather than encrypting
// an empty byte slice that would behave as "set but useless".
func TestUserSettings_Update_EmptyKeyRejected(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	empty := "   "
	_, err := svc.Update(context.Background(), userID, service.UpdateUserSettingsInput{ClaudeAPIKey: &empty})
	if err == nil {
		t.Fatal("Update with empty key succeeded, want error")
	}
}

// TestUserSettings_Update_PreferredProviderValidation rejects unknown
// values so the AI service's switch statement doesn't fall through to a
// silent default.
func TestUserSettings_Update_PreferredProviderValidation(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	bogus := "gpt-4"
	_, err := svc.Update(context.Background(), userID, service.UpdateUserSettingsInput{PreferredProvider: &bogus})
	if !errors.Is(err, service.ErrInvalidProvider) {
		t.Fatalf("err = %v, want ErrInvalidProvider", err)
	}
}

// TestUserSettings_Update_OllamaURL set + clear round-trip.
func TestUserSettings_Update_OllamaURL(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()
	url := "http://my-ollama:11434"
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{OllamaBaseURL: &url})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v.OllamaBaseURL == nil || *v.OllamaBaseURL != url {
		t.Errorf("OllamaBaseURL = %v, want %q", v.OllamaBaseURL, url)
	}
	v2, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{ClearOllamaURL: true})
	if err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if v2.OllamaBaseURL != nil {
		t.Errorf("OllamaBaseURL = %v after clear, want nil", v2.OllamaBaseURL)
	}
}

// TestUserSettings_Update_OpenAIProvider covers the #354 surface: "openai"
// is an accepted provider, the key encrypts-at-rest + redacts-on-read like
// the Claude key, and the base URL round-trips set + clear.
func TestUserSettings_Update_OpenAIProvider(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()

	provider := "openai"
	key := "sk-proxy-abc"
	url := "http://localhost:8080/v1"
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{
		PreferredProvider: &provider,
		OpenAIAPIKey:      &key,
		OpenAIBaseURL:     &url,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v.PreferredProvider != "openai" {
		t.Errorf("PreferredProvider = %q, want openai", v.PreferredProvider)
	}
	if !v.OpenAIAPIKeySet {
		t.Errorf("OpenAIAPIKeySet = false after setting, want true")
	}
	if v.OpenAIBaseURL == nil || *v.OpenAIBaseURL != url {
		t.Errorf("OpenAIBaseURL = %v, want %q", v.OpenAIBaseURL, url)
	}

	resolved, err := svc.Resolve(ctx, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Provider != "openai" {
		t.Errorf("Resolve.Provider = %q, want openai", resolved.Provider)
	}
	if resolved.OpenAIKey != key {
		t.Errorf("Resolve.OpenAIKey = %q, want %q", resolved.OpenAIKey, key)
	}
	if resolved.OpenAIBaseURL != url {
		t.Errorf("Resolve.OpenAIBaseURL = %q, want %q", resolved.OpenAIBaseURL, url)
	}

	// Clear key + url leaves the flags off and the resolver empty.
	v2, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{
		ClearOpenAIAPIKey: true,
		ClearOpenAIURL:    true,
	})
	if err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if v2.OpenAIAPIKeySet {
		t.Errorf("OpenAIAPIKeySet = true after clear, want false")
	}
	if v2.OpenAIBaseURL != nil {
		t.Errorf("OpenAIBaseURL = %v after clear, want nil", v2.OpenAIBaseURL)
	}
}

// TestUserSettings_DifferentUsers — each user keeps separate settings.
func TestUserSettings_DifferentUsers(t *testing.T) {
	svc, userA := newUserSettingsSvc(t)
	g := openTestDB(t)
	userB := seedTestUser(t, g)
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userB).Delete(&model.UserSettings{})
	})

	ctx := context.Background()
	keyA := "sk-A"
	if _, err := svc.Update(ctx, userA, service.UpdateUserSettingsInput{ClaudeAPIKey: &keyA}); err != nil {
		t.Fatalf("Update A: %v", err)
	}
	vB, err := svc.Get(ctx, userB)
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if vB.ClaudeAPIKeySet {
		t.Errorf("user B inherited user A's key flag")
	}
	resolvedB, _ := svc.Resolve(ctx, userB)
	if resolvedB.ClaudeKey != "" {
		t.Errorf("user B sees user A's key: %q", resolvedB.ClaudeKey)
	}
}
