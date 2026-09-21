package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

func strp(s string) *string { return &s }

// TestClaudeCategorizer_Roundtrip drives a batch of candidates through the
// categorizer against a fake Messages endpoint. Locks down: the payload sent
// upstream carries only merchant/description/amount-sign fields (ADR-0022
// §4 PII minimization) plus the category slug list, and a hallucinated slug
// absent from options is dropped rather than trusted.
func TestClaudeCategorizer_Roundtrip(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("x-api-key"), "sk-test"; got != want {
			t.Errorf("x-api-key = %q, want %q", got, want)
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		inner := `{"verdicts":[` +
			`{"ref":0,"category_slug":"groceries","confidence":0.92},` +
			`{"ref":1,"category_slug":"not-a-real-slug","confidence":0.99},` +
			`{"ref":2,"category_slug":"dining","confidence":0.3}` +
			`]}`
		writeExtractText(t, w, inner)
	}))
	defer srv.Close()

	cat, err := NewClaudeCategorizer(ClaudeConfig{APIKey: "sk-test", Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("NewClaudeCategorizer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	candidates := []categorization.Candidate{
		{Ref: 0, MerchantName: strp("Whole Foods"), DescriptionClean: strp("WFM #123"), AmountSign: categorization.AmountSignDebit},
		{Ref: 1, MerchantName: strp("Mystery Co"), AmountSign: categorization.AmountSignDebit},
		{Ref: 2, DescriptionClean: strp("Cafe Luna"), AmountSign: categorization.AmountSignDebit},
	}
	options := []categorization.CategoryOption{
		{ID: 1, Slug: "groceries"},
		{ID: 2, Slug: "dining"},
	}

	verdicts, err := cat.Categorize(ctx, candidates, options)
	if err != nil {
		t.Fatalf("Categorize: %v", err)
	}

	// The hallucinated "not-a-real-slug" verdict must be dropped.
	if len(verdicts) != 2 {
		t.Fatalf("verdicts = %d, want 2 (hallucinated slug dropped)", len(verdicts))
	}
	if verdicts[0].Ref != 0 || verdicts[0].CategorySlug != "groceries" || verdicts[0].Confidence != 0.92 {
		t.Errorf("verdicts[0] = %+v", verdicts[0])
	}
	if verdicts[1].Ref != 2 || verdicts[1].CategorySlug != "dining" || verdicts[1].Confidence != 0.3 {
		t.Errorf("verdicts[1] = %+v", verdicts[1])
	}

	// Payload minimization: no account/user identifiers should ever appear —
	// the request body should only reference merchant/description text,
	// amount-sign words, category slugs, and the JSON schema/prompt text.
	bodyStr := string(gotBody)
	for _, forbidden := range []string{"account_id", "user_id", "\"amount\":", "transaction_id"} {
		if strings.Contains(bodyStr, forbidden) {
			t.Errorf("request body contains forbidden field %q: %s", forbidden, bodyStr)
		}
	}
	if !strings.Contains(bodyStr, "Whole Foods") || !strings.Contains(bodyStr, "groceries") {
		t.Errorf("request body missing expected candidate/category text: %s", bodyStr)
	}

	if cat.Name() != "claude" {
		t.Errorf("Name() = %q, want claude", cat.Name())
	}
}

func TestClaudeCategorizer_EmptyCandidates(t *testing.T) {
	cat, err := NewClaudeCategorizer(ClaudeConfig{APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("NewClaudeCategorizer: %v", err)
	}
	verdicts, err := cat.Categorize(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Categorize: %v", err)
	}
	if verdicts != nil {
		t.Errorf("verdicts = %+v, want nil for empty candidates (no API call)", verdicts)
	}
}

func TestClaudeCategorizer_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid key"})
	}))
	defer srv.Close()

	cat, err := NewClaudeCategorizer(ClaudeConfig{APIKey: "bad-key", Endpoint: srv.URL})
	if err != nil {
		t.Fatalf("NewClaudeCategorizer: %v", err)
	}
	_, err = cat.Categorize(context.Background(),
		[]categorization.Candidate{{Ref: 0, MerchantName: strp("X"), AmountSign: categorization.AmountSignDebit}},
		[]categorization.CategoryOption{{ID: 1, Slug: "groceries"}})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want wrapping ErrUnauthorized", err)
	}
}
