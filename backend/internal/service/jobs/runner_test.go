package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureLog is a logf that records formatted lines for assertions.
type captureLog struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureLog) logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (c *captureLog) joined() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.lines, "\n")
}

// fakeNotifier records every alert it receives.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []string
	// panicOnCall lets a test prove the runner survives a broken notifier.
	panicOnCall bool
}

func (f *fakeNotifier) Notify(_ context.Context, subject, detail string) {
	f.mu.Lock()
	f.calls = append(f.calls, subject+"|"+detail)
	f.mu.Unlock()
	if f.panicOnCall {
		panic("notifier boom")
	}
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestRunner_runOnce(t *testing.T) {
	tests := []struct {
		name         string
		run          func(ctx context.Context) (string, error)
		wantNotify   int
		wantLogParts []string
	}{
		{
			name:         "success logs outcome, no alert",
			run:          func(context.Context) (string, error) { return "did 3 things", nil },
			wantNotify:   0,
			wantLogParts: []string{"[job] demo: ok", "did 3 things"},
		},
		{
			name:         "error logs failure and alerts",
			run:          func(context.Context) (string, error) { return "", errors.New("boom") },
			wantNotify:   1,
			wantLogParts: []string{"[job] demo: FAILED", "boom"},
		},
		{
			name:         "panic is recovered, logged as failure, and alerts",
			run:          func(context.Context) (string, error) { panic("kaboom") },
			wantNotify:   1,
			wantLogParts: []string{"[job] demo: FAILED", "panic: kaboom"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &captureLog{}
			notif := &fakeNotifier{}
			r := NewRunner(log.logf, notif)

			r.runOnce(context.Background(), Job{Name: "demo", Run: tt.run})

			if got := notif.count(); got != tt.wantNotify {
				t.Fatalf("notify count = %d, want %d", got, tt.wantNotify)
			}
			out := log.joined()
			for _, part := range tt.wantLogParts {
				if !strings.Contains(out, part) {
					t.Fatalf("log missing %q; got:\n%s", part, out)
				}
			}
		})
	}
}

// A panicking notifier must not take down the runner.
func TestRunner_runOnce_survivesNotifierPanic(t *testing.T) {
	log := &captureLog{}
	notif := &fakeNotifier{panicOnCall: true}
	r := NewRunner(log.logf, notif)

	// Must not panic out of runOnce.
	r.runOnce(context.Background(), Job{
		Name: "demo",
		Run:  func(context.Context) (string, error) { return "", errors.New("boom") },
	})

	if notif.count() != 1 {
		t.Fatalf("notifier should have been called once, got %d", notif.count())
	}
	if !strings.Contains(log.joined(), "notifier panicked") {
		t.Fatalf("expected a 'notifier panicked' log line; got:\n%s", log.joined())
	}
}

// A nil notifier is safe: failures are logged, nothing is alerted, no panic.
func TestRunner_runOnce_nilNotifier(t *testing.T) {
	log := &captureLog{}
	r := NewRunner(log.logf, nil)
	r.runOnce(context.Background(), Job{
		Name: "demo",
		Run:  func(context.Context) (string, error) { return "", errors.New("boom") },
	})
	if !strings.Contains(log.joined(), "FAILED") {
		t.Fatalf("expected FAILED log; got:\n%s", log.joined())
	}
}

// Start runs a job repeatedly on its interval and stops cleanly on ctx cancel.
func TestRunner_Start_runsThenStopsOnCancel(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	r := NewRunner(func(string, ...any) {}, nil)
	r.Register(Job{
		Name:     "tick",
		Interval: 10 * time.Millisecond,
		Run: func(context.Context) (string, error) {
			mu.Lock()
			runs++
			mu.Unlock()
			return "ok", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)

	// Let it tick several times.
	time.Sleep(55 * time.Millisecond)
	cancel()

	mu.Lock()
	atCancel := runs
	mu.Unlock()
	if atCancel < 2 {
		t.Fatalf("expected >=2 runs before cancel, got %d", atCancel)
	}

	// After cancel it must stop; allow at most one in-flight tick.
	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	afterCancel := runs
	mu.Unlock()
	if afterCancel > atCancel+1 {
		t.Fatalf("job kept running after cancel: %d -> %d", atCancel, afterCancel)
	}
}

// InitialDelay is honored: a job with a delay longer than the observation
// window doesn't run, and cancel during the delay is clean.
func TestRunner_Start_initialDelayAndCancel(t *testing.T) {
	var mu sync.Mutex
	ran := false
	r := NewRunner(func(string, ...any) {}, nil)
	r.Register(Job{
		Name:         "delayed",
		Interval:     10 * time.Millisecond,
		InitialDelay: time.Hour, // far beyond the window
		Run: func(context.Context) (string, error) {
			mu.Lock()
			ran = true
			mu.Unlock()
			return "ok", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if ran {
		t.Fatal("job ran before its InitialDelay elapsed")
	}
}
