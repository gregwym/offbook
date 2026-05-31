// contract-check verifies that every apiClient.<method>('<path>') call in
// frontend/src/api/*.ts maps to a registered route in
// backend/internal/handler/*.go. Run from the repo root or set
// CONTRACT_REPO_ROOT.
//
// Catches the class of bug behind #266 (Insights 404 → /investments/portfolio
// removed per ADR-0013 but still called) and #268 (Investments page → same
// pattern). See epic #270 and issue #273 for the full plan.
//
// Sub-second runtime, no DB, no compose stack. Wire into `make verify`.
//
// Exit codes:
//
//	0  every frontend call maps to a backend route
//	1  one or more frontend calls have no matching backend route
//	2  scan failure (couldn't read files, regex didn't compile, etc.)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	root := resolveRoot()

	backend, err := scanBackend(filepath.Join(root, "backend", "internal", "handler"))
	if err != nil {
		fail("scan backend: %v", err)
	}
	frontend, err := scanFrontend(filepath.Join(root, "frontend", "src", "api"))
	if err != nil {
		fail("scan frontend: %v", err)
	}

	backendSet := make(map[route]struct{}, len(backend))
	for _, r := range backend {
		backendSet[r.normalized()] = struct{}{}
	}

	var missing []frontendCall
	for _, fc := range frontend {
		if _, ok := backendSet[fc.route.normalized()]; !ok {
			missing = append(missing, fc)
		}
	}

	if len(missing) == 0 {
		fmt.Printf("contract-check: OK — %d frontend calls all map to backend routes (%d registered).\n",
			len(frontend), len(backend))
		os.Exit(0)
	}

	sort.Slice(missing, func(i, j int) bool {
		if missing[i].file != missing[j].file {
			return missing[i].file < missing[j].file
		}
		return missing[i].line < missing[j].line
	})

	fmt.Fprintf(os.Stderr, "contract-check: FAIL — %d frontend calls have no matching backend route:\n", len(missing))
	for _, fc := range missing {
		fmt.Fprintf(os.Stderr, "  %s:%d  %s %s\n", relpath(fc.file, root), fc.line, fc.method, fc.path)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Either the backend route was removed (add it back or migrate the frontend caller)")
	fmt.Fprintln(os.Stderr, "or the frontend path has a typo. See AGENTS.md and epic #270 for context.")
	os.Exit(1)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "contract-check: "+format+"\n", args...)
	os.Exit(2)
}

// route is a (method, path) pair. Path may carry param names from either
// side (Gin's :id or TS template ${id}); normalize() strips them.
type route struct {
	method string
	path   string
}

func (r route) normalized() route {
	return route{method: strings.ToUpper(r.method), path: normalizePath(r.path)}
}

// normalizePath collapses any param segment — Gin `:foo` or TS `${foo}` —
// to a single placeholder so param-name drift between the two sides
// doesn't cause spurious mismatches.
//
//	/accounts/:id            → /accounts/{}
//	/accounts/${id}/shares   → /accounts/{}/shares
//	/h/insights/net-worth    → /h/insights/net-worth   (unchanged)
func normalizePath(p string) string {
	p = templateParamRE.ReplaceAllString(p, "{}")
	p = colonParamRE.ReplaceAllString(p, "{}")
	return strings.TrimRight(p, "/")
}

var (
	templateParamRE = regexp.MustCompile(`\$\{[^}]+\}`)
	colonParamRE    = regexp.MustCompile(`:[A-Za-z_][A-Za-z0-9_]*`)
)

// ---------- backend scan ----------

// matches `<recv>.GET("/path", ...)` or `<recv>.Group("/prefix")` capture groups:
//
//	1 receiver name
//	2 method (GET|POST|PUT|PATCH|DELETE) — empty for Group
//	3 path literal
var backendRouteRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\.(GET|POST|PUT|PATCH|DELETE|Group)\(\s*"([^"]+)"`)

// matches `<recv> := <other>.Group("/prefix")` so we can prefix subsequent
// routes registered on the new receiver.
var groupAssignRE = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*:=\s*([A-Za-z_][A-Za-z0-9_]*)\.Group\(\s*"([^"]+)"\s*\)`)

func scanBackend(dir string) ([]backendRoute, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var routes []backendRoute
	for _, f := range files {
		// Skip test files — only production registrations count.
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		routes = append(routes, scanBackendFile(f, string(body))...)
	}
	return routes, nil
}

type backendRoute struct {
	route
	file string
}

func scanBackendFile(file, body string) []backendRoute {
	// Track group prefixes by receiver name. The two known cases are
	//   r := g.Group("/h")
	// in ai_handler.go and household_aggregator_handler.go — `r` then
	// hosts routes that should be prefixed with `/h`. The Register `g`
	// parameter itself never gets a prefix from inside the handler file
	// (the prefix `/api/v1` lives in router.go and applies uniformly to
	// every Register call, so we drop it from both sides).
	prefixes := map[string]string{}
	for _, m := range groupAssignRE.FindAllStringSubmatch(body, -1) {
		recv, parent, path := m[1], m[2], m[3]
		// Compose with the parent's prefix if any (handles g.Group("/h")
		// where g itself was assigned from another Group earlier).
		prefixes[recv] = strings.TrimRight(prefixes[parent]+path, "/")
	}

	var routes []backendRoute
	for _, m := range backendRouteRE.FindAllStringSubmatchIndex(body, -1) {
		recv := body[m[2]:m[3]]
		verb := body[m[4]:m[5]]
		path := body[m[6]:m[7]]
		if verb == "Group" {
			continue // already captured by groupAssignRE
		}
		full := prefixes[recv] + path
		routes = append(routes, backendRoute{
			route: route{method: verb, path: full},
			file:  file,
		})
	}
	return routes
}

// ---------- frontend scan ----------

// matches `apiClient.<method>(<...>)` where the first arg is a string or
// template literal. `(?s)` lets `.` match newlines so multi-line calls
// (see accountShares.ts line 28) are caught. `[^(]*` between the method
// and the opening paren swallows any generic type args, including the
// nested ones that broke a tighter `<[^>]*>` pattern (e.g.
// `apiClient.put<ApiItem<AccountShare> | ”>(`). Capture groups:
//
//	1 method (get|post|put|patch|delete)
//	2 path literal (without surrounding quotes/backticks)
var frontendCallRE = regexp.MustCompile(`(?s)apiClient\.(get|post|put|patch|delete)[^(]*\(\s*['` + "`" + `"]([^'` + "`" + `"]+)['` + "`" + `"]`)

type frontendCall struct {
	route
	file string
	line int
}

func scanFrontend(dir string) ([]frontendCall, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.ts"))
	if err != nil {
		return nil, err
	}
	var calls []frontendCall
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		calls = append(calls, scanFrontendFile(f, string(body))...)
	}
	return calls, nil
}

func scanFrontendFile(file, body string) []frontendCall {
	var calls []frontendCall
	for _, m := range frontendCallRE.FindAllStringSubmatchIndex(body, -1) {
		method := strings.ToUpper(body[m[2]:m[3]])
		path := body[m[4]:m[5]]
		line := 1 + strings.Count(body[:m[0]], "\n")
		calls = append(calls, frontendCall{
			route: route{method: method, path: path},
			file:  file,
			line:  line,
		})
	}
	return calls
}

// ---------- misc ----------

func resolveRoot() string {
	if v := os.Getenv("CONTRACT_REPO_ROOT"); v != "" {
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		fail("getwd: %v", err)
	}
	// Walk up until we find a go.mod sibling of backend/ — supports being
	// called from anywhere inside the repo.
	for dir := wd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "backend", "internal", "handler")); err == nil {
			return dir
		}
	}
	fail("could not locate repo root (no backend/internal/handler found above %s; set CONTRACT_REPO_ROOT)", wd)
	return ""
}

func relpath(file, root string) string {
	if rel, err := filepath.Rel(root, file); err == nil {
		return rel
	}
	return file
}
