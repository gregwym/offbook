// Package jobs is a small in-app scheduler for periodic background maintenance
// tasks — grace-period household purge (#359), ingestion-jobs purge (#337), the
// price refresh (#338), and future periodic needs.
//
// It generalizes the pattern the price scheduler established (ADR-0020): the
// server is one long-lived process per instance that already holds a DB handle
// and config, so an in-app runner is simpler to operate than per-FLAVOR systemd
// timers for the self-hosted deployment model. A Job is just a name + interval +
// func; the Runner tickers each one on its own goroutine, logs every outcome,
// recovers panics, and forwards failures to a Notifier (the seam the M13
// notifier #360 plugs into).
package jobs

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"
)

// Notifier is alerted when a job run fails. It is the injection seam for the
// M13 notifier (#360); until that lands, main wires LogNotifier.
type Notifier interface {
	// Notify delivers a failure alert. Implementations should return promptly
	// and must not panic — a notifier failure must never take down the runner
	// (the Runner also guards the call).
	Notify(ctx context.Context, subject, detail string)
}

// Job is a named periodic task.
type Job struct {
	// Name identifies the job in logs and alerts. Keep it short and stable.
	Name string
	// Interval between runs. Must be > 0 for a repeating job.
	Interval time.Duration
	// InitialDelay before the first run, so boot work (migrations, first
	// requests) isn't competing with the job. Zero runs almost immediately.
	InitialDelay time.Duration
	// Run does the work and returns a short human-readable outcome
	// ("purged 3 members, removed 5 shares") plus an error. A returned error —
	// or a panic, which the Runner recovers — is logged and sent to the Notifier.
	Run func(ctx context.Context) (string, error)
}

// Runner owns a set of jobs, each ticking on its own goroutine.
type Runner struct {
	jobs     []Job
	logf     func(format string, args ...any)
	notifier Notifier
}

// NewRunner builds a runner. logf defaults to log.Printf when nil; a nil
// notifier means failures are logged but not alerted (still safe).
func NewRunner(logf func(format string, args ...any), notifier Notifier) *Runner {
	if logf == nil {
		logf = log.Printf
	}
	return &Runner{logf: logf, notifier: notifier}
}

// Register adds a job. Call before Start.
func (r *Runner) Register(j Job) { r.jobs = append(r.jobs, j) }

// Start launches every registered job in its own goroutine and returns
// immediately. All jobs stop when ctx is canceled.
func (r *Runner) Start(ctx context.Context) {
	for _, j := range r.jobs {
		go r.loop(ctx, j)
	}
}

// loop runs one job after its initial delay, then on its interval, until ctx
// is canceled.
func (r *Runner) loop(ctx context.Context, j Job) {
	if j.InitialDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(j.InitialDelay):
		}
	}
	r.runOnce(ctx, j)

	if j.Interval <= 0 {
		return // defensively: a repeating job needs a positive interval
	}
	ticker := time.NewTicker(j.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx, j)
		}
	}
}

// runOnce executes a single run, guarding against panics so one bad job can
// neither crash the server nor starve its siblings. Failures are logged and
// forwarded to the Notifier; successes are logged with their outcome + duration.
func (r *Runner) runOnce(ctx context.Context, j Job) {
	start := time.Now()
	outcome, err := r.safeRun(ctx, j)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		r.logf("[job] %s: FAILED after %s: %v", j.Name, elapsed, err)
		r.notify(ctx, j.Name, err)
		return
	}
	r.logf("[job] %s: ok in %s — %s", j.Name, elapsed, outcome)
}

// safeRun invokes the job func, converting a panic into an error so it is
// treated like any other failure (logged + alerted) rather than crashing.
func (r *Runner) safeRun(ctx context.Context, j Job) (outcome string, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
		}
	}()
	return j.Run(ctx)
}

// notify forwards a failure to the Notifier, guarding the notifier itself so an
// alerting failure never kills the job loop.
func (r *Runner) notify(ctx context.Context, name string, err error) {
	if r.notifier == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			r.logf("[job] %s: notifier panicked: %v", name, p)
		}
	}()
	r.notifier.Notify(ctx, "offbook job failed: "+name, err.Error())
}

// LogNotifier is the default Notifier: it logs the alert with a distinct,
// greppable prefix. The M13 notifier (#360) replaces it with real delivery
// (ntfy / webhook / email) — main injects the replacement, nothing else changes.
type LogNotifier struct {
	Logf func(format string, args ...any)
}

// Notify logs the alert at a prominent, greppable prefix.
func (n LogNotifier) Notify(_ context.Context, subject, detail string) {
	logf := n.Logf
	if logf == nil {
		logf = log.Printf
	}
	logf("[job][ALERT] %s: %s", subject, detail)
}
