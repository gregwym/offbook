package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gregwym/offbook/backend/internal/service/ingestion"
)

// DefaultExtractMaxTokens is generous — a dense statement page can hold dozens
// of rows, each a JSON object. Bounded so a runaway response can't bill
// unboundedly.
const DefaultExtractMaxTokens = 8192

// extractSystemPrompt steers the model to emit ONLY the JSON object. The import
// layer is the trust boundary, so the prompt optimizes for parseability, not
// for the model to do validation we re-do deterministically anyway.
const extractSystemPrompt = `You extract transactions from financial statements (bank, credit card, or brokerage) into strict JSON.
Rules:
- Output ONLY a single JSON object. No prose, no markdown code fences.
- Schema: {"rows":[{"date":"YYYY-MM-DD","amount":"-12.34","description":"...","confidence":0.0}],"doc_totals":["-1234.56"],"notes":""}
- amount: a decimal string. Negative = money out (debit/withdrawal/purchase); positive = money in (credit/deposit). Keep full precision; no currency symbols or thousands separators.
- date: the transaction date as YYYY-MM-DD. If only a posting date is shown, use it.
- description: the merchant or memo text, cleaned of statement noise.
- confidence: 0–1, your certainty in THAT row. Lower it when a field was ambiguous, smudged, or inferred.
- doc_totals: any statement totals you can read (closing balance delta, total debits/credits) as signed decimal strings, for reconciliation. Empty array if none.
- Include every transaction line you can read. Do NOT invent rows to make totals match.`

const extractUserPrompt = `Extract all transactions from this statement as the JSON object described. Output only the JSON.`

// ClaudeExtractor implements ingestion.Extractor against the Anthropic Messages
// API (non-streaming) using document/image content blocks. No SDK — net/http,
// matching ClaudeProvider's house style. It re-parses every model-proposed row
// through ingestion.NewRow so the deterministic validators (not the LLM) decide
// admissibility.
type ClaudeExtractor struct {
	apiKey    string
	endpoint  string
	model     string
	maxTokens int
	http      *http.Client
}

// NewClaudeExtractor reuses ClaudeConfig (APIKey required; Endpoint/HTTPClient
// for tests). Model defaults to DefaultClaudeModel.
func NewClaudeExtractor(cfg ClaudeConfig) (*ClaudeExtractor, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("ai: claude extractor requires APIKey")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultClaudeEndpoint
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &ClaudeExtractor{
		apiKey:    cfg.APIKey,
		endpoint:  endpoint,
		model:     DefaultClaudeModel,
		maxTokens: DefaultExtractMaxTokens,
		http:      client,
	}, nil
}

// Name is persisted in ingestion_jobs.provider.
func (e *ClaudeExtractor) Name() string { return "claude" }

// Handles reports the document types this extractor can send to Claude: PDFs,
// common image formats, and CSV/text (the unmappable-CSV→AI fallback).
func (e *ClaudeExtractor) Handles(mime string) bool {
	switch normalizeMediaType(mime) {
	case "application/pdf",
		"image/png", "image/jpeg", "image/gif", "image/webp",
		"text/csv", "application/csv", "text/plain":
		return true
	default:
		return false
	}
}

// Extract sends the document plus the extraction instruction in one
// non-streaming Messages call, parses the JSON object, and re-validates each
// row through ingestion.NewRow into a neutral *ingestion.Extraction.
func (e *ClaudeExtractor) Extract(ctx context.Context, doc ingestion.Document) (*ingestion.Extraction, error) {
	docBlock, source, err := blockAndSource(doc)
	if err != nil {
		return nil, err
	}

	reqBody := claudeExtractRequest{
		Model:     e.model,
		MaxTokens: e.maxTokens,
		System:    extractSystemPrompt,
		Messages: []claudeExtractMessage{{
			Role:    "user",
			Content: []claudeContentBlock{docBlock, {Type: "text", Text: extractUserPrompt}},
		}},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ai: marshal extract request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("ai: build extract request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", e.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := e.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: claude extract request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(body)))
		}
		return nil, fmt.Errorf("ai: claude extract returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var apiResp claudeExtractResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ai: parse claude extract response: %w", err)
	}
	text := apiResp.text()
	if strings.TrimSpace(text) == "" {
		return nil, ingestion.ErrEmptyExtraction
	}
	parsed, err := parseExtractionJSON(text)
	if err != nil {
		return nil, err
	}
	if len(parsed.Rows) == 0 {
		return nil, ingestion.ErrEmptyExtraction
	}

	// Re-validate every proposed row through the shared deterministic parser —
	// a hallucinated date/amount becomes an error row, never a transaction.
	ext := &ingestion.Extraction{Source: source, DocTotals: parsed.DocTotals}
	for i, r := range parsed.Rows {
		ext.Rows = append(ext.Rows, ingestion.NewRow(i+1, r.Date, r.Amount, r.Description, r.Confidence))
	}
	return ext, nil
}

// blockAndSource maps a document to its Anthropic content block and the
// transactions.source value the extracted rows carry.
func blockAndSource(doc ingestion.Document) (claudeContentBlock, string, error) {
	mt := normalizeMediaType(doc.MIME)
	switch mt {
	case "application/pdf":
		return base64Block("document", mt, doc.Data), "pdf", nil
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return base64Block("image", mt, doc.Data), "photo", nil
	case "text/csv", "application/csv", "text/plain":
		return claudeContentBlock{Type: "text", Text: string(doc.Data)}, "csv", nil
	default:
		return claudeContentBlock{}, "", fmt.Errorf("%w: %q", ingestion.ErrUnsupportedMediaType, doc.MIME)
	}
}

func base64Block(blockType, mediaType string, data []byte) claudeContentBlock {
	return claudeContentBlock{
		Type: blockType,
		Source: &claudeBlockSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      base64.StdEncoding.EncodeToString(data),
		},
	}
}

// normalizeMediaType lowercases and strips any "; charset=..." parameter.
func normalizeMediaType(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return mime
}

// parseExtractionJSON tolerates a leading/trailing markdown fence or stray
// prose by extracting the outermost {...} object before unmarshaling.
func parseExtractionJSON(text string) (*documentExtraction, error) {
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
	var out documentExtraction
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("ai: extractor returned non-JSON: %w", err)
	}
	return &out, nil
}

// ── Anthropic Messages API wire structs (extraction subset) ──

type claudeExtractRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    string                 `json:"system,omitempty"`
	Messages  []claudeExtractMessage `json:"messages"`
}

type claudeExtractMessage struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeContentBlock struct {
	Type   string             `json:"type"`
	Text   string             `json:"text,omitempty"`
	Source *claudeBlockSource `json:"source,omitempty"`
}

type claudeBlockSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type claudeExtractResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

// text concatenates all text content blocks in the response.
func (r claudeExtractResponse) text() string {
	var b strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

var _ ingestion.Extractor = (*ClaudeExtractor)(nil)
