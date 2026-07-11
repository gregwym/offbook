package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/config"
)

// TestNtfyNotifier_Notify_RequestShape asserts method, Title header, and body
// match ntfy's expected POST shape.
func TestNtfyNotifier_Notify_RequestShape(t *testing.T) {
	var gotMethod, gotTitle, gotBody, gotPriority string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NtfyNotifier{URL: srv.URL}
	n.Notify(context.Background(), "offbook job failed: household-purge", "purge: boom")

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotTitle != "offbook job failed: household-purge" {
		t.Errorf("Title header = %q, want subject", gotTitle)
	}
	if gotPriority != "high" {
		t.Errorf("Priority header = %q, want high", gotPriority)
	}
	if gotBody != "purge: boom" {
		t.Errorf("body = %q, want detail", gotBody)
	}
}

// TestNtfyNotifier_Notify_EmptyURLNoOp asserts a Notifier with no URL simply
// does nothing (no request, no panic) — this is the "unconfigured" path.
func TestNtfyNotifier_Notify_EmptyURLNoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	n := NtfyNotifier{URL: ""}
	n.Notify(context.Background(), "subject", "detail")

	if called {
		t.Error("expected no HTTP request for an unconfigured NtfyNotifier")
	}
}

// TestNtfyNotifier_Notify_ServerErrorDoesNotPanic proves a failing delivery
// (5xx, or an unreachable server) is swallowed, not surfaced — a broken
// notifier must never break the caller.
func TestNtfyNotifier_Notify_ServerErrorDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var loggedCalls int
	n := NtfyNotifier{URL: srv.URL, Logf: func(format string, args ...any) { loggedCalls++ }}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Notify panicked: %v", r)
		}
	}()
	n.Notify(context.Background(), "subject", "detail")

	if loggedCalls == 0 {
		t.Error("expected a log call on a 5xx response")
	}
}

// TestWebhookNotifier_Notify_RequestShape asserts method, Content-Type header,
// and JSON body fields match the generic webhook contract.
func TestWebhookNotifier_Notify_RequestShape(t *testing.T) {
	var gotMethod, gotContentType string
	var gotPayload webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := WebhookNotifier{URL: srv.URL}
	n.Notify(context.Background(), "backup failed", "pg_dump failed")

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotPayload.Subject != "backup failed" {
		t.Errorf("payload.subject = %q, want %q", gotPayload.Subject, "backup failed")
	}
	if gotPayload.Detail != "pg_dump failed" {
		t.Errorf("payload.detail = %q, want %q", gotPayload.Detail, "pg_dump failed")
	}
	if gotPayload.Time == "" {
		t.Error("payload.time is empty")
	}
	if _, err := time.Parse(time.RFC3339, gotPayload.Time); err != nil {
		t.Errorf("payload.time %q is not RFC3339: %v", gotPayload.Time, err)
	}
}

// TestWebhookNotifier_Notify_EmptyURLNoOp mirrors the ntfy no-op-when-
// unconfigured contract.
func TestWebhookNotifier_Notify_EmptyURLNoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	n := WebhookNotifier{URL: ""}
	n.Notify(context.Background(), "subject", "detail")

	if called {
		t.Error("expected no HTTP request for an unconfigured WebhookNotifier")
	}
}

// TestMulti_Notify_FansOutToAll asserts every configured notifier receives
// the call.
func TestMulti_Notify_FansOutToAll(t *testing.T) {
	var calls []string
	a := recordingNotifier{name: "a", calls: &calls}
	b := recordingNotifier{name: "b", calls: &calls}

	m := Multi{a, b}
	m.Notify(context.Background(), "subject", "detail")

	if len(calls) != 2 {
		t.Fatalf("calls = %v, want 2 entries", calls)
	}
}

// TestThrottled_SuppressesRepeatWithinWindow proves the core dedup contract:
// a second Notify for the same subject inside Window is suppressed, a
// different subject goes through immediately, and the same subject goes
// through again once Window has elapsed. Uses an injectable clock (no sleeps)
// per repo convention for deterministic tests.
func TestThrottled_SuppressesRepeatWithinWindow(t *testing.T) {
	var calls []string
	rec := recordingNotifier{name: "inner", calls: &calls}

	clock := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	th := &Throttled{
		Inner:  rec,
		Window: time.Hour,
		now:    func() time.Time { return clock },
	}

	th.Notify(context.Background(), "plaid item error: item-1", "boom")
	if len(calls) != 1 {
		t.Fatalf("after first Notify: calls = %v, want 1", calls)
	}

	// Same subject, still inside the window → suppressed.
	clock = clock.Add(30 * time.Minute)
	th.Notify(context.Background(), "plaid item error: item-1", "boom again")
	if len(calls) != 1 {
		t.Fatalf("after second Notify (same subject, inside window): calls = %v, want still 1", calls)
	}

	// Different subject → goes through immediately regardless of window.
	th.Notify(context.Background(), "plaid item error: item-2", "different item")
	if len(calls) != 2 {
		t.Fatalf("after Notify with different subject: calls = %v, want 2", calls)
	}

	// Same original subject, now past the window → goes through again.
	clock = clock.Add(31 * time.Minute) // total elapsed since first send: 61m > 1h window
	th.Notify(context.Background(), "plaid item error: item-1", "boom a third time")
	if len(calls) != 3 {
		t.Fatalf("after window elapsed: calls = %v, want 3", calls)
	}
}

// TestThrottled_DefaultWindowWhenUnset proves Window<=0 falls back to the 6h
// default rather than throttling with a zero window (which would effectively
// disable throttling).
func TestThrottled_DefaultWindowWhenUnset(t *testing.T) {
	var calls []string
	rec := recordingNotifier{name: "inner", calls: &calls}

	clock := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	th := &Throttled{
		Inner: rec,
		now:   func() time.Time { return clock },
	}

	th.Notify(context.Background(), "subject", "detail")
	clock = clock.Add(time.Hour) // well inside the 6h default
	th.Notify(context.Background(), "subject", "detail again")

	if len(calls) != 1 {
		t.Fatalf("calls = %v, want 1 (default window should still be throttling)", calls)
	}
}

// TestBuild_NoURLsConfiguredLogsOnly proves Build with neither URL set
// returns a notifier that only logs (never panics, never makes a request).
func TestBuild_NoURLsConfiguredLogsOnly(t *testing.T) {
	var logged []string
	n := Build(testConfig(), func(format string, args ...any) {
		logged = append(logged, format)
	})

	n.Notify(context.Background(), "subject", "detail")

	if len(logged) == 0 {
		t.Error("expected a log call when no notify URLs are configured")
	}
}

// TestBuild_WiresConfiguredTargets proves Build fans out to whichever of
// ntfy/webhook are configured.
func TestBuild_WiresConfiguredTargets(t *testing.T) {
	var ntfyHit, webhookHit bool
	ntfySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ntfyHit = true
	}))
	defer ntfySrv.Close()
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookHit = true
	}))
	defer webhookSrv.Close()

	cfg := testConfig()
	cfg.NotifyNtfyURL = ntfySrv.URL
	cfg.NotifyWebhookURL = webhookSrv.URL

	n := Build(cfg, nil)
	n.Notify(context.Background(), "subject", "detail")

	if !ntfyHit {
		t.Error("expected ntfy endpoint to be hit")
	}
	if !webhookHit {
		t.Error("expected webhook endpoint to be hit")
	}
}

// recordingNotifier is a test double satisfying the Notifier interface.
type recordingNotifier struct {
	name  string
	calls *[]string
}

func (r recordingNotifier) Notify(_ context.Context, subject, detail string) {
	*r.calls = append(*r.calls, r.name+":"+subject+":"+detail)
}

// testConfig returns a zero-value config.Config — every notify field defaults
// to "unconfigured" (empty URLs, 0 throttle minutes) unless a test overrides
// it explicitly.
func testConfig() config.Config {
	return config.Config{}
}
