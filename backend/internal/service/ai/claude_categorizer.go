package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

// DefaultCategorizeMaxTokens bounds a batch categorization response — one
// short JSON object per candidate, so even a full batch (AI_CATEGORIZE_BATCH_SIZE
// candidates) stays well under this.
const DefaultCategorizeMaxTokens = 2048

const categorizeSystemPrompt = `You categorize financial transactions into a fixed set of categories.
Rules:
- Output ONLY a single JSON object. No prose, no markdown code fences.
- Schema: {"verdicts":[{"ref":1,"category_slug":"groceries","confidence":0.0}]}
- ref: the integer "ref" from the candidate you're answering.
- category_slug: MUST be one of the provided category slugs, exactly as given. Never invent a slug.
- confidence: 0-1, how certain you are this is the right category for this merchant. Lower it for ambiguous or unfamiliar merchant strings.
- Skip a candidate entirely (omit it from verdicts) if you have no reasonable guess — do not guess randomly.
- debit means money leaving the account (a purchase/expense); credit means money coming in (income/refund/repayment). Use this to disambiguate, e.g. don't categorize a credit as a grocery purchase.`

// ClaudeCategorizer implements categorization.Categorizer against the
// Anthropic Messages API (non-streaming), mirroring ClaudeExtractor's single-
// shot call style. No SDK — net/http, matching the package's house style.
type ClaudeCategorizer struct {
	apiKey    string
	endpoint  string
	model     string
	maxTokens int
	http      *http.Client
}

// NewClaudeCategorizer reuses ClaudeConfig (APIKey required; Endpoint/
// HTTPClient for tests). Model defaults to DefaultClaudeModel.
func NewClaudeCategorizer(cfg ClaudeConfig) (*ClaudeCategorizer, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("ai: claude categorizer requires APIKey")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultClaudeEndpoint
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &ClaudeCategorizer{
		apiKey:    cfg.APIKey,
		endpoint:  endpoint,
		model:     DefaultClaudeModel,
		maxTokens: DefaultCategorizeMaxTokens,
		http:      client,
	}, nil
}

// Name is persisted in ai_categorization_verdicts.provider.
func (c *ClaudeCategorizer) Name() string { return "claude" }

// Categorize sends every candidate plus the allowed category slugs in one
// non-streaming Messages call and parses the JSON verdict list. A verdict
// naming a slug absent from options is dropped — the deterministic caller
// (not the model) decides admissibility, same posture as ClaudeExtractor
// re-validating rows through ingestion.NewRow.
func (c *ClaudeCategorizer) Categorize(ctx context.Context, candidates []categorization.Candidate, options []categorization.CategoryOption) ([]categorization.Verdict, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	validSlugs := make(map[string]struct{}, len(options))
	for _, o := range options {
		validSlugs[o.Slug] = struct{}{}
	}

	userPrompt := buildCategorizeUserPrompt(candidates, options)
	reqBody := claudeExtractRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    categorizeSystemPrompt,
		Messages: []claudeExtractMessage{{
			Role:    "user",
			Content: []claudeContentBlock{{Type: "text", Text: userPrompt}},
		}},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal categorize request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("ai: build categorize request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: claude categorize request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(body)))
		}
		return nil, fmt.Errorf("ai: claude categorize returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var apiResp claudeExtractResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ai: parse claude categorize response: %w", err)
	}
	text := apiResp.text()
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	parsed, err := parseCategorizeJSON(text)
	if err != nil {
		return nil, err
	}

	out := make([]categorization.Verdict, 0, len(parsed.Verdicts))
	for _, v := range parsed.Verdicts {
		if _, ok := validSlugs[v.CategorySlug]; !ok {
			// Hallucinated or malformed slug — never trust the model's word
			// for admissibility, same posture as row re-validation elsewhere
			// in this package.
			continue
		}
		conf := v.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		out = append(out, categorization.Verdict{
			Ref:          v.Ref,
			CategorySlug: v.CategorySlug,
			Confidence:   conf,
		})
	}
	return out, nil
}

func buildCategorizeUserPrompt(candidates []categorization.Candidate, options []categorization.CategoryOption) string {
	var b strings.Builder
	b.WriteString("Categories (slug — use exactly): ")
	for i, o := range options {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(o.Slug)
	}
	b.WriteString("\n\nTransactions:\n")
	for _, c := range candidates {
		fmt.Fprintf(&b, "ref=%d %s merchant=%q description=%q\n",
			c.Ref, c.AmountSign, safeStr(c.MerchantName), safeStr(c.DescriptionClean))
	}
	b.WriteString("\nRespond with the JSON object described in the system prompt, one verdict per transaction you can confidently categorize.")
	return b.String()
}

func safeStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// parseCategorizeJSON tolerates a leading/trailing markdown fence or stray
// prose by extracting the outermost {...} object before unmarshaling — same
// tolerant approach as parseExtractionJSON.
func parseCategorizeJSON(text string) (*categorizeVerdicts, error) {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if start := strings.IndexByte(s, '{'); start > 0 {
		if end := strings.LastIndexByte(s, '}'); end >= start {
			s = s[start : end+1]
		}
	}
	var out categorizeVerdicts
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("ai: categorizer returned non-JSON: %w", err)
	}
	return &out, nil
}

type categorizeVerdicts struct {
	Verdicts []categorizeVerdict `json:"verdicts"`
}

type categorizeVerdict struct {
	Ref          int     `json:"ref"`
	CategorySlug string  `json:"category_slug"`
	Confidence   float64 `json:"confidence"`
}

var _ categorization.Categorizer = (*ClaudeCategorizer)(nil)
