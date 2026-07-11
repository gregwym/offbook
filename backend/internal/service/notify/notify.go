// Package notify implements the #360 monitoring/alerting seam: ntfy and
// generic-webhook delivery, throttled per-subject so a broken item or a stuck
// scheduler doesn't page hourly forever.
//
// This package deliberately does NOT import internal/service/jobs or
// internal/service/plaid — those packages each declare their own small
// Notifier interface with the same method shape (Notify(ctx, subject,
// detail)), and Go interfaces are structural, so any Notifier built here
// satisfies both without a dependency edge back into this package.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gregwym/offbook/backend/internal/config"
)

// defaultThrottleWindow is used by Build when NotifyThrottleMinutes is unset
// (<= 0).
const defaultThrottleWindow = 6 * time.Hour

// defaultHTTPTimeout bounds how long a single ntfy/webhook delivery attempt
// may take — these run on background job goroutines and must not hang them.
const defaultHTTPTimeout = 10 * time.Second

// Notifier is alerted when something worth paging the instance owner about
// happens. Matches the shape expected by internal/service/jobs.Notifier and
// internal/service/plaid.Notifier (see package doc).
type Notifier interface {
	Notify(ctx context.Context, subject, detail string)
}

// logf is the shared shape for the optional logging hook every notifier in
// this package accepts. nil defaults to log.Printf.
type logFunc func(format string, args ...any)

func (f logFunc) orDefault() func(format string, args ...any) {
	if f == nil {
		return log.Printf
	}
	return f
}

// NtfyNotifier delivers alerts to an ntfy (ntfy.sh or self-hosted) topic URL
// via a plain POST. Never panics; delivery failures are logged, not returned
// (Notify has no error return by design — the caller can't act on a failure
// anyway).
type NtfyNotifier struct {
	URL        string
	HTTPClient *http.Client
	Logf       func(format string, args ...any)
}

// Notify posts detail as the ntfy message body with subject as the Title
// header.
func (n NtfyNotifier) Notify(ctx context.Context, subject, detail string) {
	logf := logFunc(n.Logf).orDefault()
	if n.URL == "" {
		return
	}
	client := n.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewBufferString(detail))
	if err != nil {
		logf("notify: ntfy: build request: %v", err)
		return
	}
	req.Header.Set("Title", subject)
	req.Header.Set("Priority", "high")

	resp, err := client.Do(req)
	if err != nil {
		logf("notify: ntfy: delivery failed: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		logf("notify: ntfy: delivery got status %d", resp.StatusCode)
	}
}

// webhookPayload is the generic JSON body posted to WebhookNotifier.URL.
type webhookPayload struct {
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
	Time    string `json:"time"`
}

// WebhookNotifier delivers alerts as generic JSON to any HTTP endpoint —
// self-host friendly, no vendor-specific shape (Slack/Discord/etc. can sit
// behind a small adapter if needed later; out of scope for #360).
type WebhookNotifier struct {
	URL        string
	HTTPClient *http.Client
	Logf       func(format string, args ...any)
}

// Notify posts {"subject","detail","time"} as JSON.
func (n WebhookNotifier) Notify(ctx context.Context, subject, detail string) {
	logf := logFunc(n.Logf).orDefault()
	if n.URL == "" {
		return
	}
	client := n.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	body, err := json.Marshal(webhookPayload{
		Subject: subject,
		Detail:  detail,
		Time:    time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		logf("notify: webhook: marshal payload: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
	if err != nil {
		logf("notify: webhook: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logf("notify: webhook: delivery failed: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		logf("notify: webhook: delivery got status %d", resp.StatusCode)
	}
}

// Multi fans an alert out to every element, sequentially (these are
// low-frequency alert calls; a goroutine-per-target isn't worth the
// complexity). One target's failure doesn't block the others since each
// implementation swallows its own errors.
type Multi []Notifier

// Notify delivers to every notifier in order.
func (m Multi) Notify(ctx context.Context, subject, detail string) {
	for _, n := range m {
		if n == nil {
			continue
		}
		n.Notify(ctx, subject, detail)
	}
}

// Throttled dedupes repeat alerts for the same subject string within Window,
// so an erroring item doesn't page hourly forever. now defaults to time.Now
// (overridable for deterministic tests).
type Throttled struct {
	Inner  Notifier
	Window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// Notify delivers to Inner unless the same subject was already sent within
// Window.
func (t *Throttled) Notify(ctx context.Context, subject, detail string) {
	if t.Inner == nil {
		return
	}
	nowFn := t.now
	if nowFn == nil {
		nowFn = time.Now
	}
	window := t.Window
	if window <= 0 {
		window = defaultThrottleWindow
	}

	now := nowFn()
	t.mu.Lock()
	if t.lastSent == nil {
		t.lastSent = make(map[string]time.Time)
	}
	if last, ok := t.lastSent[subject]; ok && now.Sub(last) < window {
		t.mu.Unlock()
		return
	}
	t.lastSent[subject] = now
	t.mu.Unlock()

	t.Inner.Notify(ctx, subject, detail)
}

// logOnlyNotifier mirrors jobs.LogNotifier's behavior locally so this package
// never needs to import internal/service/jobs (see package doc).
type logOnlyNotifier struct {
	Logf func(format string, args ...any)
}

func (n logOnlyNotifier) Notify(_ context.Context, subject, detail string) {
	logf := logFunc(n.Logf).orDefault()
	logf("[notify][ALERT] %s: %s", subject, detail)
}

// Build returns the configured Notifier: a Throttled wrapper around whichever
// of ntfy/webhook have a URL set in cfg. If neither is configured, it returns
// a notifier that only logs (mirrors jobs.LogNotifier's behavior so a fresh
// instance with no monitoring configured still surfaces failures somewhere).
func Build(cfg config.Config, logf func(format string, args ...any)) *Throttled {
	var targets Multi
	if cfg.NotifyNtfyURL != "" {
		targets = append(targets, NtfyNotifier{URL: cfg.NotifyNtfyURL, Logf: logf})
	}
	if cfg.NotifyWebhookURL != "" {
		targets = append(targets, WebhookNotifier{URL: cfg.NotifyWebhookURL, Logf: logf})
	}

	var inner Notifier
	if len(targets) == 0 {
		inner = logOnlyNotifier{Logf: logf}
	} else {
		inner = targets
	}

	window := time.Duration(cfg.NotifyThrottleMinutes) * time.Minute
	if window <= 0 {
		window = defaultThrottleWindow
	}

	return &Throttled{Inner: inner, Window: window}
}
