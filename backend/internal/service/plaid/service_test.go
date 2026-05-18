package plaid_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/model"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

// fakePlaid returns an httptest.Server that mimics enough of the Plaid REST
// shape for /link/token/create and /item/public_token/exchange to exercise
// the SDK wiring end-to-end. Tests assert on the request body and choose
// the response.
func fakePlaid(t *testing.T) (*httptest.Server, *recordedRequests) {
	t.Helper()
	rec := &recordedRequests{}

	mux := http.NewServeMux()
	mux.HandleFunc("/link/token/create", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.linkBody = body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"link_token": "link-sandbox-fake-token",
			"expiration": "2026-05-17T20:00:00Z",
			"request_id": "req-1",
		})
	})
	mux.HandleFunc("/item/public_token/exchange", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.exchangeBody = body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-sandbox-fake-secret",
			"item_id":      "item-fake-1",
			"request_id":   "req-2",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Plaid call: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

type recordedRequests struct {
	linkBody     map[string]any
	exchangeBody map[string]any
}

// fakeRepo is an in-memory PlaidItemRepository sufficient for service tests.
// We use it instead of the real GORM repo so service unit tests don't need
// a live Postgres — that path is covered by integration tests separately.
type fakeRepo struct {
	created []*model.PlaidItem
}

func (r *fakeRepo) Create(_ context.Context, item *model.PlaidItem) error {
	item.ID = int64(len(r.created) + 1)
	r.created = append(r.created, item)
	return nil
}

// These methods aren't exercised by Link/Exchange; satisfy the interface
// with safe no-ops so the test file compiles.
func (r *fakeRepo) GetByID(context.Context, int64, int64) (*model.PlaidItem, error) {
	return nil, nil
}
func (r *fakeRepo) GetByPlaidItemID(context.Context, int64, string) (*model.PlaidItem, error) {
	return nil, nil
}
func (r *fakeRepo) ListByUser(context.Context, int64) ([]model.PlaidItem, error) { return nil, nil }
func (r *fakeRepo) UpdateStatus(context.Context, int64, int64, string, *string) error {
	return nil
}
func (r *fakeRepo) SoftDelete(context.Context, int64, int64) error { return nil }
func (r *fakeRepo) UpdateCursor(context.Context, int64, int64, string, time.Time) error {
	return nil
}

// (fakeRepo implements PlaidItemRepository — the transaction-repo methods
// added in #62 / #63 live on a separate interface and aren't exercised here.)

func newTestKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestService_CreateLinkToken_HappyPath(t *testing.T) {
	srv, rec := fakePlaid(t)
	client, err := plaidsvc.NewSDKClient(plaidsvc.Config{
		ClientID: "cid",
		Secret:   "csec",
		Env:      srv.URL, // SDK accepts any URL as an Environment override
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	box, err := crypto.NewSecretBox(newTestKey())
	if err != nil {
		t.Fatalf("secretbox: %v", err)
	}
	svc := plaidsvc.NewService(client, box, &fakeRepo{}, nil, nil, nil, nil, nil)

	tok, err := svc.CreateLinkToken(context.Background(), 42)
	if err != nil {
		t.Fatalf("CreateLinkToken: %v", err)
	}
	if tok.Token != "link-sandbox-fake-token" {
		t.Errorf("token = %q", tok.Token)
	}
	if tok.Expiration.IsZero() {
		t.Error("expiration should be non-zero")
	}
	// user.client_user_id must include the bound user_id — the per-user
	// fraud signal is the whole reason Plaid asks for it.
	user, _ := rec.linkBody["user"].(map[string]any)
	if cu, _ := user["client_user_id"].(string); !strings.Contains(cu, "42") {
		t.Errorf("client_user_id = %q, want it to encode user_id 42", cu)
	}
}

func TestService_ExchangePublicToken_HappyPath(t *testing.T) {
	srv, rec := fakePlaid(t)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	repo := &fakeRepo{}
	svc := plaidsvc.NewService(client, box, repo, nil, nil, nil, nil, nil)

	item, err := svc.ExchangePublicToken(context.Background(), 7, "public-sandbox-xyz")
	if err != nil {
		t.Fatalf("ExchangePublicToken: %v", err)
	}
	if item.PlaidItemID != "item-fake-1" {
		t.Errorf("plaid_item_id = %q", item.PlaidItemID)
	}
	if item.UserID != 7 {
		t.Errorf("user_id = %d, want 7", item.UserID)
	}
	if rec.exchangeBody["public_token"] != "public-sandbox-xyz" {
		t.Errorf("public_token forwarded = %v", rec.exchangeBody["public_token"])
	}

	// The persisted row's AccessTokenEnc must NOT contain the plaintext
	// token. This is the whole point of ADR-0010.
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 persisted item, got %d", len(repo.created))
	}
	persisted := repo.created[0]
	if bytes.Contains(persisted.AccessTokenEnc, []byte("access-sandbox-fake-secret")) {
		t.Fatal("access_token was stored in plaintext (or merely concealed by encoding)")
	}
	// And it must decrypt cleanly.
	decrypted, err := box.Decrypt(persisted.AccessTokenEnc)
	if err != nil {
		t.Fatalf("decrypt persisted token: %v", err)
	}
	if string(decrypted) != "access-sandbox-fake-secret" {
		t.Errorf("decrypted = %q", string(decrypted))
	}
}

func TestService_ExchangePublicToken_RejectsEmpty(t *testing.T) {
	srv, _ := fakePlaid(t)
	client, _ := plaidsvc.NewSDKClient(plaidsvc.Config{ClientID: "cid", Secret: "csec", Env: srv.URL})
	box, _ := crypto.NewSecretBox(newTestKey())
	svc := plaidsvc.NewService(client, box, &fakeRepo{}, nil, nil, nil, nil, nil)

	if _, err := svc.ExchangePublicToken(context.Background(), 1, "   "); err != plaidsvc.ErrInvalidPublicToken {
		t.Fatalf("expected ErrInvalidPublicToken, got %v", err)
	}
}

func TestService_NotConfigured(t *testing.T) {
	svc := plaidsvc.NewService(nil, nil, nil, nil, nil, nil, nil, nil)
	if svc.Configured() {
		t.Fatal("expected !Configured")
	}
	if _, err := svc.CreateLinkToken(context.Background(), 1); err != plaidsvc.ErrNotConfigured {
		t.Errorf("link: %v", err)
	}
	if _, err := svc.ExchangePublicToken(context.Background(), 1, "p"); err != plaidsvc.ErrNotConfigured {
		t.Errorf("exchange: %v", err)
	}
}
