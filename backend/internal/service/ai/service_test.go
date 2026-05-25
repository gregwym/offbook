package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service/ai"
)

// stubProvider emits a fixed delta sequence so the service test doesn't
// need a real HTTP server. Records the request it received for sanity
// assertions on system prompt + history forwarding.
type stubProvider struct {
	deltas    []ai.Delta
	lastReq   ai.Request
	streamErr error
	mu        sync.Mutex
}

func (p *stubProvider) Name() string { return "stub" }

func (p *stubProvider) Stream(_ context.Context, req ai.Request) (<-chan ai.Delta, error) {
	p.mu.Lock()
	p.lastReq = req
	p.mu.Unlock()
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	ch := make(chan ai.Delta, len(p.deltas))
	for _, d := range p.deltas {
		ch <- d
	}
	close(ch)
	return ch, nil
}

// stubThreadRepo is an in-memory ai_threads store keyed by (userID, id).
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
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.threads[id]
	if !ok || t.UserID != userID {
		return repository.ErrNotFound
	}
	t.Title = &title
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

// stubMessageRepo is an in-memory ai_messages store.
type stubMessageRepo struct {
	mu     sync.Mutex
	msgs   []model.AIMessage
	nextID int64
}

func newStubMessageRepo() *stubMessageRepo {
	return &stubMessageRepo{nextID: 1}
}

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

// TestService_SendMessage_StubProvider exercises the happy path: provider
// streams three deltas + a done; the service forwards them, then persists
// the assembled assistant message with a non-empty context snapshot.
func TestService_SendMessage_StubProvider(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()

	prov := &stubProvider{
		deltas: []ai.Delta{
			{Text: "Hello"},
			{Text: ", "},
			{Text: "world!"},
			{Done: true, FinishReason: "end_turn", Usage: ai.Usage{InputTokens: 17, OutputTokens: 9}},
		},
	}

	// Builder is nil — the service skips context build cleanly. The
	// AC requires "non-empty context_snapshot" so we plumb a builder via
	// a separate path below. For the first assertion we just want the
	// delta-relay shape.
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(prov))

	const userID int64 = 42
	thr, err := svc.CreateThread(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}

	events, err := svc.SendMessage(context.Background(), userID, thr.ID, "hi there")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	var (
		concatenated strings.Builder
		gotDone      bool
		doneEvent    ai.DonePayload
	)
	for ev := range events {
		switch ev.Type {
		case ai.SSEDelta:
			var p ai.DeltaPayload
			if err := json.Unmarshal(ev.Data, &p); err != nil {
				t.Fatalf("delta unmarshal: %v", err)
			}
			concatenated.WriteString(p.Text)
		case ai.SSEDone:
			gotDone = true
			if err := json.Unmarshal(ev.Data, &doneEvent); err != nil {
				t.Fatalf("done unmarshal: %v", err)
			}
		case ai.SSEError:
			t.Fatalf("unexpected error event: %s", string(ev.Data))
		}
	}

	if !gotDone {
		t.Fatal("stream ended without a Done event")
	}
	if got := concatenated.String(); got != "Hello, world!" {
		t.Errorf("streamed text = %q, want %q", got, "Hello, world!")
	}
	if doneEvent.FinishReason != "end_turn" {
		t.Errorf("finish_reason = %q, want end_turn", doneEvent.FinishReason)
	}

	// Persisted: user message + assistant message + correct concatenation.
	persisted, _ := msgs.ListByThread(context.Background(), thr.ID)
	if len(persisted) != 2 {
		t.Fatalf("persisted len = %d, want 2 (user + assistant)", len(persisted))
	}
	if persisted[0].Role != string(ai.RoleUser) || persisted[0].Content != "hi there" {
		t.Errorf("user message = %+v", persisted[0])
	}
	if persisted[1].Role != string(ai.RoleAssistant) || persisted[1].Content != "Hello, world!" {
		t.Errorf("assistant message = %+v", persisted[1])
	}
	if doneEvent.MessageID != persisted[1].ID {
		t.Errorf("done.message_id = %d, want %d", doneEvent.MessageID, persisted[1].ID)
	}

	// Request sanity: system prompt set, history forwarded ending with our user turn.
	if prov.lastReq.System == "" {
		t.Error("provider got empty system prompt")
	}
	if n := len(prov.lastReq.Messages); n != 1 {
		t.Fatalf("provider got %d messages, want 1 (the new user turn)", n)
	}
	if got := prov.lastReq.Messages[0].Content; got != "hi there" {
		t.Errorf("provider user message = %q, want %q", got, "hi there")
	}
}

// TestService_SendMessage_PersistsContextSnapshot verifies the assistant
// message's context_snapshot column is populated when a builder is wired.
func TestService_SendMessage_PersistsContextSnapshot(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	prov := &stubProvider{
		deltas: []ai.Delta{
			{Text: "ok"},
			{Done: true, FinishReason: "end_turn"},
		},
	}

	// Pass a builder with all-nil sub-services. Build() returns an empty
	// Context (no error), which JSON-marshals to a non-empty object — the
	// AC's "context_snapshot is non-empty" check passes.
	builder := ai.NewContextBuilder(nil, nil, nil, nil)
	svc := ai.NewService(threads, msgs, builder, ai.StaticResolver(prov))

	const userID int64 = 7
	thr, _ := svc.CreateThread(context.Background(), userID, nil)
	events, err := svc.SendMessage(context.Background(), userID, thr.ID, "hi")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	for range events {
		// drain
	}

	persisted, _ := msgs.ListByThread(context.Background(), thr.ID)
	if len(persisted) != 2 {
		t.Fatalf("len = %d, want 2", len(persisted))
	}
	snap := persisted[1].ContextSnapshot
	if len(snap) == 0 {
		t.Fatal("context_snapshot is empty on assistant message")
	}
	// Snapshot must be valid JSON and a non-empty object.
	var parsed map[string]any
	if err := json.Unmarshal(snap, &parsed); err != nil {
		t.Fatalf("context_snapshot not JSON: %v (raw=%s)", err, string(snap))
	}
	if len(parsed) == 0 {
		t.Fatalf("context_snapshot is an empty object: %s", string(snap))
	}
}

// TestService_SendMessage_NoProvider returns ErrNoProvider before opening
// a stream — so the handler can map to a 503 rather than holding an SSE
// connection open just to immediately close it. nil resolver and a
// resolver that returns nil both map to ErrNoProvider.
func TestService_SendMessage_NoProvider(t *testing.T) {
	t.Run("nil resolver", func(t *testing.T) {
		svc := ai.NewService(newStubThreadRepo(), newStubMessageRepo(), nil, nil)
		thr, _ := svc.CreateThread(context.Background(), 1, nil)
		_, err := svc.SendMessage(context.Background(), 1, thr.ID, "hi")
		if !errors.Is(err, ai.ErrNoProvider) {
			t.Fatalf("err = %v, want ErrNoProvider", err)
		}
	})
	t.Run("resolver returns nil", func(t *testing.T) {
		svc := ai.NewService(newStubThreadRepo(), newStubMessageRepo(), nil, ai.StaticResolver(nil))
		thr, _ := svc.CreateThread(context.Background(), 1, nil)
		_, err := svc.SendMessage(context.Background(), 1, thr.ID, "hi")
		if !errors.Is(err, ai.ErrNoProvider) {
			t.Fatalf("err = %v, want ErrNoProvider", err)
		}
	})
}

// TestService_SendMessage_EmptyContent rejects whitespace-only messages
// before persisting anything or invoking the provider.
func TestService_SendMessage_EmptyContent(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	prov := &stubProvider{deltas: []ai.Delta{{Done: true}}}
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(prov))

	thr, _ := svc.CreateThread(context.Background(), 1, nil)
	_, err := svc.SendMessage(context.Background(), 1, thr.ID, "   \t  ")
	if !errors.Is(err, ai.ErrEmptyMessage) {
		t.Fatalf("err = %v, want ErrEmptyMessage", err)
	}
	if got, _ := msgs.ListByThread(context.Background(), thr.ID); len(got) != 0 {
		t.Errorf("got %d persisted, want 0", len(got))
	}
}

// TestService_CrossUserAccess — user B asking for user A's thread by ID
// gets ErrThreadNotFound (not a 403 — same status as a missing thread,
// to defeat enumeration). Applies to ListMessages, GetThread, SendMessage.
func TestService_CrossUserAccess(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	prov := &stubProvider{deltas: []ai.Delta{{Done: true}}}
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(prov))

	const userA, userB int64 = 1, 2
	aThr, _ := svc.CreateThread(context.Background(), userA, nil)

	cases := []struct {
		name string
		run  func() error
	}{
		{"GetThread", func() error {
			_, err := svc.GetThread(context.Background(), userB, aThr.ID)
			return err
		}},
		{"ListMessages", func() error {
			_, err := svc.ListMessages(context.Background(), userB, aThr.ID)
			return err
		}},
		{"SendMessage", func() error {
			_, err := svc.SendMessage(context.Background(), userB, aThr.ID, "leak?")
			return err
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if !errors.Is(err, ai.ErrThreadNotFound) {
				t.Errorf("got %v, want ErrThreadNotFound", err)
			}
		})
	}

	// User B's ListThreads MUST NOT include user A's thread.
	bThreads, err := svc.ListThreads(context.Background(), userB)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	for _, th := range bThreads {
		if th.ID == aThr.ID {
			t.Errorf("user B sees user A's thread %d in ListThreads", th.ID)
		}
	}
}

// TestService_SendMessage_ProviderError surfaces a mid-stream error as a
// terminal SSEError event. No assistant message is persisted.
func TestService_SendMessage_ProviderError(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	prov := &stubProvider{
		deltas: []ai.Delta{
			{Text: "partial"},
			{Err: errors.New("upstream blew up")},
		},
	}
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(prov))

	thr, _ := svc.CreateThread(context.Background(), 1, nil)
	events, err := svc.SendMessage(context.Background(), 1, thr.ID, "hi")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	var sawError bool
	for ev := range events {
		if ev.Type == ai.SSEError {
			sawError = true
			if !strings.Contains(string(ev.Data), "upstream blew up") {
				t.Errorf("error event = %s, want it to mention upstream", string(ev.Data))
			}
		}
	}
	if !sawError {
		t.Fatal("expected SSEError event from mid-stream provider err")
	}
	// User message persisted; no assistant message.
	persisted, _ := msgs.ListByThread(context.Background(), thr.ID)
	if len(persisted) != 1 || persisted[0].Role != string(ai.RoleUser) {
		t.Errorf("persisted = %+v, want just the user message", persisted)
	}
}
