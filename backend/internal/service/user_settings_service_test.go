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
	if v.APITokenSet {
		t.Errorf("APITokenSet = true on fresh user, want false")
	}
	if v.PreferredProvider != "claude" {
		t.Errorf("PreferredProvider = %q, want 'claude'", v.PreferredProvider)
	}
}

// TestUserSettings_Update_StoresAndHidesToken verifies the encrypt-at-rest +
// redact-on-read contract for the unified API token: the plaintext is
// recoverable via Resolve(), but Get() never returns it — only APITokenSet=true.
func TestUserSettings_Update_StoresAndHidesToken(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()

	token := "sk-ant-test-12345"
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{
		APIToken: &token,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !v.APITokenSet {
		t.Errorf("APITokenSet = false after setting, want true")
	}

	// Resolve must be able to recover the plaintext (router-side provider
	// resolver depends on it).
	resolved, err := svc.Resolve(ctx, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Token != token {
		t.Errorf("Resolve.Token = %q, want %q", resolved.Token, token)
	}
}

// TestUserSettings_Update_ClearToken removes the stored ciphertext.
func TestUserSettings_Update_ClearToken(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()
	k := "sk-test"
	if _, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{APIToken: &k}); err != nil {
		t.Fatalf("Update set: %v", err)
	}
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{ClearAPIToken: true})
	if err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if v.APITokenSet {
		t.Errorf("APITokenSet = true after clear, want false")
	}
	resolved, _ := svc.Resolve(ctx, userID)
	if resolved.Token != "" {
		t.Errorf("Resolve.Token = %q after clear, want empty", resolved.Token)
	}
}

// TestUserSettings_Update_EmptyTokenRejected — empty plaintext is a
// user-error (probably a stripped paste); we reject rather than encrypting
// an empty byte slice that would behave as "set but useless".
func TestUserSettings_Update_EmptyTokenRejected(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	empty := "   "
	_, err := svc.Update(context.Background(), userID, service.UpdateUserSettingsInput{APIToken: &empty})
	if err == nil {
		t.Fatal("Update with empty token succeeded, want error")
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

// TestUserSettings_Update_Endpoint set + clear round-trip.
func TestUserSettings_Update_Endpoint(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()
	url := "http://my-ollama:11434"
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{APIEndpoint: &url})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v.APIEndpoint == nil || *v.APIEndpoint != url {
		t.Errorf("APIEndpoint = %v, want %q", v.APIEndpoint, url)
	}
	resolved, _ := svc.Resolve(ctx, userID)
	if resolved.Endpoint != url {
		t.Errorf("Resolve.Endpoint = %q, want %q", resolved.Endpoint, url)
	}
	v2, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{ClearAPIEndpoint: true})
	if err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if v2.APIEndpoint != nil {
		t.Errorf("APIEndpoint = %v after clear, want nil", v2.APIEndpoint)
	}
}

// TestUserSettings_Update_Protocol covers picking an OpenAI-compatible
// protocol alongside its endpoint + token in one patch.
func TestUserSettings_Update_Protocol(t *testing.T) {
	svc, userID := newUserSettingsSvc(t)
	ctx := context.Background()

	provider := "openai"
	token := "sk-proxy-abc"
	url := "http://localhost:8080/v1"
	v, err := svc.Update(ctx, userID, service.UpdateUserSettingsInput{
		PreferredProvider: &provider,
		APIToken:          &token,
		APIEndpoint:       &url,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v.PreferredProvider != "openai" {
		t.Errorf("PreferredProvider = %q, want openai", v.PreferredProvider)
	}

	resolved, err := svc.Resolve(ctx, userID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Provider != "openai" {
		t.Errorf("Resolve.Provider = %q, want openai", resolved.Provider)
	}
	if resolved.Token != token {
		t.Errorf("Resolve.Token = %q, want %q", resolved.Token, token)
	}
	if resolved.Endpoint != url {
		t.Errorf("Resolve.Endpoint = %q, want %q", resolved.Endpoint, url)
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
	if _, err := svc.Update(ctx, userA, service.UpdateUserSettingsInput{APIToken: &keyA}); err != nil {
		t.Fatalf("Update A: %v", err)
	}
	vB, err := svc.Get(ctx, userB)
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if vB.APITokenSet {
		t.Errorf("user B inherited user A's token flag")
	}
	resolvedB, _ := svc.Resolve(ctx, userB)
	if resolvedB.Token != "" {
		t.Errorf("user B sees user A's token: %q", resolvedB.Token)
	}
}
