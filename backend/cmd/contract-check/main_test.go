package main

import (
	"testing"
)

func TestNormalizePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/accounts", "/accounts"},
		{"/accounts/:id", "/accounts/{}"},
		{"/accounts/${id}", "/accounts/{}"},
		{"/accounts/${accountId}/shares/${householdID}", "/accounts/{}/shares/{}"},
		{"/accounts/:id/shares/:householdID", "/accounts/{}/shares/{}"},
		{"/h/insights/net-worth", "/h/insights/net-worth"},
		// trailing-slash variants should normalize identically
		{"/accounts/", "/accounts"},
		{"/", ""},
	}
	for _, c := range cases {
		if got := normalizePath(c.in); got != c.want {
			t.Errorf("normalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestScanFrontendFile(t *testing.T) {
	body := "" +
		"import { apiClient } from './client'\n" +
		"export async function a() {\n" +
		"  const res = await apiClient.get<ApiList<Foo>>('/accounts', { params })\n" +
		"  return res.data.data\n" +
		"}\n" +
		"export async function b(id: number) {\n" +
		"  await apiClient.delete(`/accounts/${id}`)\n" +
		"}\n" +
		// multi-line call — the bug we saw in accountShares.ts: nested
		// generics in the type parameter, with the URL on the next line.
		"export async function c(accountID: number, householdID: number) {\n" +
		"  const res = await apiClient.put<ApiItem<AccountShare> | ''>(\n" +
		"    `/accounts/${accountID}/shares/${householdID}`,\n" +
		"    { visibility: 'private' },\n" +
		"  )\n" +
		"  return res.data\n" +
		"}\n"

	got := scanFrontendFile("fake.ts", body)
	if len(got) != 3 {
		t.Fatalf("scanFrontendFile: got %d calls, want 3 — %+v", len(got), got)
	}

	expect := []frontendCall{
		{route: route{method: "GET", path: "/accounts"}, file: "fake.ts", line: 3},
		{route: route{method: "DELETE", path: "/accounts/${id}"}, file: "fake.ts", line: 7},
		// Multi-line: line points at where `apiClient.put` starts, not
		// where the URL string is. That's the right pointer for the
		// reviewer fixing the broken call.
		{route: route{method: "PUT", path: "/accounts/${accountID}/shares/${householdID}"}, file: "fake.ts", line: 10},
	}
	for i, want := range expect {
		if got[i].method != want.method || got[i].path != want.path || got[i].line != want.line {
			t.Errorf("call %d: got %+v, want %+v", i, got[i], want)
		}
	}
}

func TestScanBackendFile(t *testing.T) {
	body := "" +
		"package handler\n" +
		"func (h *AccountHandler) Register(g *gin.RouterGroup) {\n" +
		"  g.POST(\"/accounts\", h.Create)\n" +
		"  g.GET(\"/accounts/:id\", h.Get)\n" +
		"}\n" +
		"func (h *AIHandler) Register(g *gin.RouterGroup) {\n" +
		"  g.POST(\"/ai/threads\", h.CreateThread)\n" +
		"  // household subgroup — routes registered on r get the /h prefix\n" +
		"  r := g.Group(\"/h\")\n" +
		"  r.GET(\"/ai/threads\", h.ListSharedThreads)\n" +
		"  r.GET(\"/ai/threads/:id/messages\", h.ListSharedMessages)\n" +
		"}\n"

	got := scanBackendFile("fake.go", body)
	want := []backendRoute{
		{route: route{method: "POST", path: "/accounts"}, file: "fake.go"},
		{route: route{method: "GET", path: "/accounts/:id"}, file: "fake.go"},
		{route: route{method: "POST", path: "/ai/threads"}, file: "fake.go"},
		{route: route{method: "GET", path: "/h/ai/threads"}, file: "fake.go"},
		{route: route{method: "GET", path: "/h/ai/threads/:id/messages"}, file: "fake.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("scanBackendFile: got %d routes, want %d — %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].method != w.method || got[i].path != w.path {
			t.Errorf("route %d: got %+v, want %+v", i, got[i], w)
		}
	}
}

func TestEndToEnd_RegressionFor266And268(t *testing.T) {
	// Synthesizes the exact failure mode from epic #270: a frontend
	// caller for a route the backend never registers. This is the test
	// that, had it existed, would have failed at the time PR #240 (M10a)
	// removed /investments and /investments/portfolio.
	feBody := "" +
		"import { apiClient } from './client'\n" +
		"export const list = () => apiClient.get('/investments')\n" +
		"export const portfolio = () => apiClient.get('/investments/portfolio')\n" +
		"export const ok = () => apiClient.get('/accounts')\n"
	beBody := "" +
		"package handler\n" +
		"func (h *AccountHandler) Register(g *gin.RouterGroup) {\n" +
		"  g.GET(\"/accounts\", h.List)\n" +
		"}\n"

	frontend := scanFrontendFile("api.ts", feBody)
	backend := scanBackendFile("h.go", beBody)

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

	if len(missing) != 2 {
		t.Fatalf("expected 2 missing calls (the two investments routes), got %d: %+v", len(missing), missing)
	}
	paths := map[string]bool{}
	for _, m := range missing {
		paths[m.path] = true
	}
	if !paths["/investments"] || !paths["/investments/portfolio"] {
		t.Errorf("expected missing paths to include /investments and /investments/portfolio, got %v", paths)
	}
}
