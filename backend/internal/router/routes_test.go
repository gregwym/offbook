package router_test

// L4 #274 — backend route inventory snapshot. Builds the production Gin
// engine, walks its registered routes, and compares the sorted list
// against a golden file. Any add / remove / verb change shows up as a
// snapshot diff that a reviewer has to consciously accept — that's the
// gate that would have caught PR #240 (M10a) silently dropping the
// /investments wiring with no downstream signal.
//
// To accept a legitimate route change: run with -update.
//
//	UPDATE_GOLDEN=1 go test ./internal/router
//
// (No -update flag because integration tests in this repo don't pass
// custom flags through `make test`. An env var works everywhere.)
//
// Skips when DATABASE_URL is unset — same convention as the other
// integration tests in this tree (transaction_repo_test, etc.).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/db"
	"github.com/gregwym/offbook/backend/internal/router"
)

const goldenPath = "routes.golden"

func TestRouteInventory_Snapshot(t *testing.T) {
	gormDB := openTestDB(t)
	cfg := config.Config{
		// SessionSecret feeds the SecretBox in newUserSettingsService;
		// any non-empty value works because the test never invokes a
		// session-decoding handler.
		SessionSecret: "00000000000000000000000000000000",
		FrontendURL:   "http://localhost:5173",
	}

	gin.SetMode(gin.TestMode)
	engine := router.New(cfg, gormDB)

	got := dumpRoutes(engine)

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s (%d lines)", goldenPath, strings.Count(got, "\n"))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v (first run? UPDATE_GOLDEN=1 go test ./internal/router)", goldenPath, err)
	}

	if string(want) == got {
		return
	}

	t.Fatalf("route inventory drift detected. Diff:\n%s\n\n"+
		"If this change is intentional, refresh the golden:\n"+
		"  UPDATE_GOLDEN=1 go test ./internal/router\n"+
		"and review %s in the PR. If it was accidental — a deleted Register()\n"+
		"call, a renamed path, a missing handler wiring — fix the route side,\n"+
		"NOT the golden. See AGENTS.md → Frontend↔Backend Contract Discipline.",
		simpleDiff(string(want), got), goldenPath)
}

// dumpRoutes renders the engine's route table as one "METHOD PATH" line
// per route, sorted by (method, path). Stable across runs — Gin's
// iteration order isn't.
func dumpRoutes(e *gin.Engine) string {
	routes := e.Routes()
	lines := make([]string, len(routes))
	for i, r := range routes {
		lines[i] = fmt.Sprintf("%s %s", r.Method, r.Path)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// simpleDiff renders a line-by-line unified-style diff. Not as rich as
// diffutils but good enough to read in a `go test` failure message.
func simpleDiff(want, got string) string {
	wantSet := lineSet(want)
	gotSet := lineSet(got)
	var b strings.Builder
	for _, l := range sortedKeys(wantSet) {
		if !gotSet[l] {
			b.WriteString("- " + l + "\n")
		}
	}
	for _, l := range sortedKeys(gotSet) {
		if !wantSet[l] {
			b.WriteString("+ " + l + "\n")
		}
	}
	if b.Len() == 0 {
		return "(no line-level differences; check for trailing whitespace)"
	}
	return b.String()
}

func lineSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if l != "" {
			set[l] = true
		}
	}
	return set
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------- DB helper (mirrors openTestDB in other packages) ----------

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	loadRepoDotenv()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no DATABASE_URL set; skipping integration test")
	}
	g, err := db.Open(url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx, g); err != nil {
		t.Skipf("db.Ping: %v; skipping integration test", err)
	}
	return g
}

func loadRepoDotenv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}
