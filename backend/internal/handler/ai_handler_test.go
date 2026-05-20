package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/ai"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// HTTP-level tests for the AI handler. These cover what service-layer
// tests can't: the actual gin wire framing, SSE chunked output, and
// error-code mapping for personal + household endpoints.
//
// We don't spin up a DB. The handler is built against in-memory stubs
// for the repos + a stub provider, and the auth user_id is injected via
// a tiny test middleware that calls auth.WithUser before forwarding.

func init() {
	gin.SetMode(gin.TestMode)
}

// --- stubs -------------------------------------------------------------

type stubThreadRepo struct {
	mu      sync.Mutex
	threads map[int64]*model.AIThread
	nextID  int64
}

func newStubThreadRepo() *stubThreadRepo {
	return &stubThreadRepo{threads: map[int64]*model.AIThread{}, nextID: 1}
}

func (r *stubThreadRepo) Create(_ context.Context, t *model.AIThread) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t.ID = r.nextID
	r.nextID++
	t.CreatedAt = time.Now()
	t.UpdatedAt = t.CreatedAt
	r.threads[t.ID] = t
	return nil
}

func (r *stubThreadRepo) GetByID(_ context.Context, userID, id int64) (*model.AIThread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.threads[id]
	if !ok || t.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return t, nil
}

func (r *stubThreadRepo) ListByUser(_ context.Context, userID int64) ([]model.AIThread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.AIThread
	for _, t := range r.threads {
		if t.UserID == userID {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (r *stubThreadRepo) UpdateTitle(_ context.Context, userID, id int64, title string) error {
	return nil
}

func (r *stubThreadRepo) GetByIDForMember(_ context.Context, userID, householdID, id int64) (*model.AIThread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.threads[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if t.UserID == userID {
		return t, nil
	}
	if t.SharedWithHousehold && t.HouseholdID != nil && *t.HouseholdID == householdID {
		return t, nil
	}
	return nil, repository.ErrNotFound
}

func (r *stubThreadRepo) ListSharedByHousehold(_ context.Context, householdID int64) ([]model.AIThread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.AIThread
	for _, t := range r.threads {
		if t.SharedWithHousehold && t.HouseholdID != nil && *t.HouseholdID == householdID {
			out = append(out, *t)
		}
	}
	return out, nil
}

type stubMessageRepo struct {
	mu     sync.Mutex
	msgs   []model.AIMessage
	nextID int64
}

func newStubMessageRepo() *stubMessageRepo { return &stubMessageRepo{nextID: 1} }

func (r *stubMessageRepo) Create(_ context.Context, m *model.AIMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m.ID = r.nextID
	r.nextID++
	m.CreatedAt = time.Now()
	r.msgs = append(r.msgs, *m)
	return nil
}

func (r *stubMessageRepo) ListByThread(_ context.Context, threadID int64) ([]model.AIMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.AIMessage
	for _, m := range r.msgs {
		if m.ThreadID == threadID {
			out = append(out, m)
		}
	}
	return out, nil
}

type stubMemberRepo struct {
	// member: userID → row (nil = not a member)
	rows map[int64]*model.HouseholdMember
}

func (r *stubMemberRepo) Create(context.Context, *model.HouseholdMember) error {
	return errors.New("not used in handler tests")
}
func (r *stubMemberRepo) GetActive(context.Context, int64, int64) (*model.HouseholdMember, error) {
	return nil, repository.ErrNotFound
}
func (r *stubMemberRepo) GetByID(context.Context, int64) (*model.HouseholdMember, error) {
	return nil, repository.ErrNotFound
}
func (r *stubMemberRepo) GetMembershipForUser(_ context.Context, userID int64) (*model.HouseholdMember, error) {
	if m, ok := r.rows[userID]; ok && m != nil {
		return m, nil
	}
	return nil, repository.ErrNotFound
}
func (r *stubMemberRepo) ListActive(context.Context, int64) ([]model.HouseholdMember, error) {
	return nil, nil
}
func (r *stubMemberRepo) ListIncludingLeft(context.Context, int64) ([]model.HouseholdMember, error) {
	return nil, nil
}
func (r *stubMemberRepo) CountActiveOwners(context.Context, int64) (int64, error) { return 0, nil }
func (r *stubMemberRepo) MarkLeft(context.Context, int64, time.Time) error        { return nil }
func (r *stubMemberRepo) ClearLeft(context.Context, int64) error                  { return nil }
func (r *stubMemberRepo) UpdateRole(context.Context, int64, string) error         { return nil }

type stubProvider struct {
	deltas []ai.Delta
}

func (p *stubProvider) Name() string { return "stub" }
func (p *stubProvider) Stream(_ context.Context, _ ai.Request) (<-chan ai.Delta, error) {
	ch := make(chan ai.Delta, len(p.deltas))
	for _, d := range p.deltas {
		ch <- d
	}
	close(ch)
	return ch, nil
}

// --- test harness ------------------------------------------------------

// setupRouter wires the AI handler into a fresh gin engine with a tiny
// auth middleware that injects the supplied userID into the request
// context. Mirrors how the production router gates these routes.
func setupRouter(t *testing.T, userID int64, opts ...func(*setup)) *gin.Engine {
	t.Helper()
	s := &setup{
		threads:  newStubThreadRepo(),
		messages: newStubMessageRepo(),
		members:  &stubMemberRepo{rows: map[int64]*model.HouseholdMember{}},
		resolver: ai.StaticResolver(&stubProvider{deltas: []ai.Delta{{Done: true}}}),
	}
	for _, opt := range opts {
		opt(s)
	}
	svc := ai.NewService(s.threads, s.messages, nil, s.resolver)
	if s.household != nil {
		svc = svc.WithHouseholdAccess(s.household)
	}
	h := handler.NewAIHandler(svc, s.members)

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), userID))
		c.Next()
	})
	h.Register(api)
	return r
}

type setup struct {
	threads   *stubThreadRepo
	messages  *stubMessageRepo
	members   *stubMemberRepo
	resolver  ai.ProviderResolver
	household ai.HouseholdAccess
}

// --- personal-scope tests ---------------------------------------------

// TestPersonal_CreateThread asserts the 201 + envelope shape.
func TestPersonal_CreateThread(t *testing.T) {
	r := setupRouter(t, 42)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/threads", bytes.NewBufferString(`{}`))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Data struct {
			ID     int64 `json:"id"`
			UserID int64 `json:"user_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if got.Data.ID == 0 || got.Data.UserID != 42 {
		t.Errorf("data = %+v, want non-zero ID + UserID=42", got.Data)
	}
}

// TestPersonal_ListThreads_TotalEnvelope checks the documented list
// envelope shape — `{data: [...], total: N}` — that `.claude/rules/go-backend.md`
// requires for every list endpoint.
func TestPersonal_ListThreads_TotalEnvelope(t *testing.T) {
	threads := newStubThreadRepo()
	threads.Create(context.Background(), &model.AIThread{UserID: 42})
	threads.Create(context.Background(), &model.AIThread{UserID: 42})

	r := setupRouter(t, 42, func(s *setup) { s.threads = threads })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/threads", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Data  []map[string]any `json:"data"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Data) != 2 || got.Total != 2 {
		t.Errorf("data=%d total=%d, want 2/2", len(got.Data), got.Total)
	}
}

// TestPersonal_SendMessage_SSEWireFormat is the headline test for this
// PR: drive the full SSE response through gin, assert the framing is
// exactly `event: <type>\ndata: <json>\n\n` per event, and that the
// terminal `done` event arrives with the right shape.
//
// Uses `httptest.NewServer` (not Recorder) because gin's `c.Stream`
// requires the writer to implement `http.CloseNotifier`, which the
// recorder doesn't. We need a real TCP socket.
func TestPersonal_SendMessage_SSEWireFormat(t *testing.T) {
	threads := newStubThreadRepo()
	t1 := &model.AIThread{UserID: 42}
	_ = threads.Create(context.Background(), t1)

	r := setupRouter(t, 42, func(s *setup) {
		s.threads = threads
		s.resolver = ai.StaticResolver(&stubProvider{deltas: []ai.Delta{
			{Text: "Hello"},
			{Text: ", "},
			{Text: "world!"},
			{Done: true, FinishReason: "end_turn", Usage: ai.Usage{InputTokens: 5, OutputTokens: 3}},
		}})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	res, err := http.Post(srv.URL+"/api/v1/ai/threads/1/messages",
		"application/json",
		bytes.NewBufferString(`{"content":"hi"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %d, want 200; body=%s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(bodyBytes)
	// Each event is "event: <type>\ndata: <json>\n\n".
	events := parseSSE(t, body)
	// 3 deltas + 1 done = 4 events.
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4; body=%q", len(events), body)
	}
	for i, ev := range events[:3] {
		if ev.kind != "delta" {
			t.Errorf("event[%d] kind = %q, want delta", i, ev.kind)
		}
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(ev.data), &p); err != nil {
			t.Fatalf("event[%d] decode: %v", i, err)
		}
	}
	last := events[3]
	if last.kind != "done" {
		t.Errorf("terminal event kind = %q, want done", last.kind)
	}
	var done struct {
		FinishReason string `json:"finish_reason"`
		MessageID    int64  `json:"message_id"`
	}
	if err := json.Unmarshal([]byte(last.data), &done); err != nil {
		t.Fatalf("done decode: %v", err)
	}
	if done.FinishReason != "end_turn" {
		t.Errorf("finish_reason = %q, want end_turn", done.FinishReason)
	}
	if done.MessageID == 0 {
		t.Errorf("done.message_id is zero — the persisted assistant message wasn't created")
	}
}

// TestPersonal_SendMessage_NoProvider — the resolver returns nil; the
// handler must respond 503 NO_AI_PROVIDER (not open an SSE stream that
// immediately closes).
func TestPersonal_SendMessage_NoProvider(t *testing.T) {
	threads := newStubThreadRepo()
	_ = threads.Create(context.Background(), &model.AIThread{UserID: 42})

	r := setupRouter(t, 42, func(s *setup) {
		s.threads = threads
		s.resolver = ai.StaticResolver(nil)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ai/threads/1/messages",
		bytes.NewBufferString(`{"content":"hi"}`))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Code != "NO_AI_PROVIDER" {
		t.Errorf("code = %q, want NO_AI_PROVIDER", got.Code)
	}
	if got := w.Header().Get("Content-Type"); strings.Contains(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, must not open SSE for the error path", got)
	}
}

// --- household-scope tests --------------------------------------------

// TestHousehold_CreateSharedThread_NoMembership: user without an active
// household row gets 403 NO_HOUSEHOLD.
func TestHousehold_CreateSharedThread_NoMembership(t *testing.T) {
	r := setupRouter(t, 42) // members map is empty by default
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/h/ai/threads", bytes.NewBufferString(`{}`))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Code != "NO_HOUSEHOLD" {
		t.Errorf("code = %q, want NO_HOUSEHOLD", got.Code)
	}
}

// TestHousehold_CreateSharedThread_InactiveMembership: user has a row
// but left_at is set (in-grace) — must respond 403 MEMBERSHIP_INACTIVE.
func TestHousehold_CreateSharedThread_InactiveMembership(t *testing.T) {
	left := time.Now().Add(-time.Hour)
	r := setupRouter(t, 42, func(s *setup) {
		s.members.rows[42] = &model.HouseholdMember{
			HouseholdID: 7, UserID: 42, Role: model.RoleContributor, LeftAt: &left,
		}
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/h/ai/threads", bytes.NewBufferString(`{}`))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var got struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Code != "MEMBERSHIP_INACTIVE" {
		t.Errorf("code = %q, want MEMBERSHIP_INACTIVE", got.Code)
	}
}

// --- helpers -----------------------------------------------------------

type sseEvent struct {
	kind string
	data string
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event:"):
				ev.kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				ev.data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if ev.kind != "" {
			out = append(out, ev)
		}
	}
	return out
}
