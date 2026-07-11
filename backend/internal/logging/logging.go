// Package logging installs structured (JSON) output on the standard
// library's global logger so every existing log.Printf/log.Fatalf/log.Println
// call site across the codebase emits a structured line for free — no
// per-callsite rewrite required (#360 acceptance: "Structured (JSON) logging
// for backend + scheduled jobs").
package logging

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

// Init installs a JSON-line writer as the destination for the standard
// library's global logger. Call once, as the first line of main(), in every
// long-running or scheduled-job entry point (cmd/server, cmd/household-purge,
// cmd/ingestion-jobs-purge).
func Init() {
	log.SetFlags(0)
	log.SetOutput(jsonLineWriter{out: os.Stdout})
}

// jsonLineWriter wraps an io.Writer, turning each Write (one per log call,
// since the stdlib logger calls Write once per formatted line) into a single
// JSON line: {"time":"<RFC3339 UTC>","level":"info","msg":"<text>"}.
type jsonLineWriter struct {
	out io.Writer
}

// Write implements io.Writer. It always reports success (len(p), nil) even if
// the downstream write fails, because callers include log.Fatal — which calls
// os.Exit immediately after Write returns — and a short-write error there
// would only mask the original log message, never recover anything.
func (w jsonLineWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSuffix(string(p), "\n")
	line, err := json.Marshal(struct {
		Time  string `json:"time"`
		Level string `json:"level"`
		Msg   string `json:"msg"`
	}{
		Time:  time.Now().UTC().Format(time.RFC3339),
		Level: "info",
		Msg:   msg,
	})
	if err != nil {
		// Marshaling a plain string triple should never fail; fall back to a
		// raw write so a log line is never silently dropped.
		_, _ = w.out.Write([]byte(msg + "\n"))
		return len(p), nil
	}
	_, _ = w.out.Write(append(line, '\n'))
	return len(p), nil
}
