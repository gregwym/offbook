package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/ai"
)

// stubHouseholdAccess records what the AI service asks for and lets each
// test configure membership + the context blob independently.
type stubHouseholdAccess struct {
	mu          sync.Mutex
	activeFor   map[string]bool // key = "userID:householdID"
	contextBlob json.RawMessage
	contextErr  error

	lastBuildUserID      int64
	lastBuildHouseholdID int64
	buildCalls           int
}

func key(userID, householdID int64) string {
	return fmtKey(userID) + ":" + fmtKey(householdID)
}

func fmtKey(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (s *stubHouseholdAccess) setActive(userID, householdID int64, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeFor == nil {
		s.activeFor = map[string]bool{}
	}
	s.activeFor[key(userID, householdID)] = active
}

func (s *stubHouseholdAccess) ActiveMembership(_ context.Context, userID, householdID int64) (*model.HouseholdMember, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeFor[key(userID, householdID)] {
		return &model.HouseholdMember{HouseholdID: householdID, UserID: userID, Role: model.RoleContributor}, true, nil
	}
	return nil, false, nil
}

func (s *stubHouseholdAccess) BuildAIContext(_ context.Context, userID, householdID int64) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buildCalls++
	s.lastBuildUserID = userID
	s.lastBuildHouseholdID = householdID
	if s.contextErr != nil {
		return nil, s.contextErr
	}
	return s.contextBlob, nil
}

// TestCreateSharedThread_HappyPath: active member can create; thread is
// flagged shared_with_household and carries the household_id.
func TestCreateSharedThread_HappyPath(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	hh := &stubHouseholdAccess{}
	hh.setActive(42, 7, true)
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(&stubProvider{})).
		WithHouseholdAccess(hh)

	title := "Family Goals"
	thr, err := svc.CreateSharedThread(context.Background(), 42, 7, &title)
	if err != nil {
		t.Fatalf("CreateSharedThread: %v", err)
	}
	if !thr.SharedWithHousehold || thr.HouseholdID == nil || *thr.HouseholdID != 7 {
		t.Errorf("thread = %+v, want shared in household 7", thr)
	}
	if thr.UserID != 42 {
		t.Errorf("UserID = %d, want 42", thr.UserID)
	}
}

// TestCreateSharedThread_NonMember: a user without active membership is
// rejected with ErrNotHouseholdMember (not a leak via ErrThreadNotFound).
func TestCreateSharedThread_NonMember(t *testing.T) {
	svc := ai.NewService(newStubThreadRepo(), newStubMessageRepo(), nil, ai.StaticResolver(&stubProvider{})).
		WithHouseholdAccess(&stubHouseholdAccess{})
	_, err := svc.CreateSharedThread(context.Background(), 1, 7, nil)
	if !errors.Is(err, ai.ErrNotHouseholdMember) {
		t.Fatalf("err = %v, want ErrNotHouseholdMember", err)
	}
}

// TestSendSharedMessage_HouseholdContextUsed: when the user posts in a
// shared thread, the system prompt embeds the household context (not the
// personal one), and the persisted user message carries the user_id for
// attribution.
func TestSendSharedMessage_HouseholdContextUsed(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	hh := &stubHouseholdAccess{contextBlob: json.RawMessage(`{"household_id":7,"net_worth":"123"}`)}
	hh.setActive(42, 7, true)
	prov := &stubProvider{
		deltas: []ai.Delta{
			{Text: "Hi"},
			{Done: true, FinishReason: "end_turn"},
		},
	}
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(prov)).
		WithHouseholdAccess(hh)

	thr, err := svc.CreateSharedThread(context.Background(), 42, 7, nil)
	if err != nil {
		t.Fatalf("CreateSharedThread: %v", err)
	}
	events, err := svc.SendSharedMessage(context.Background(), 42, 7, thr.ID, "What can we afford?")
	if err != nil {
		t.Fatalf("SendSharedMessage: %v", err)
	}
	for range events {
		// drain
	}

	if hh.buildCalls != 1 {
		t.Errorf("BuildAIContext called %d times, want 1", hh.buildCalls)
	}
	if hh.lastBuildHouseholdID != 7 || hh.lastBuildUserID != 42 {
		t.Errorf("BuildAIContext got user=%d hh=%d, want 42/7", hh.lastBuildUserID, hh.lastBuildHouseholdID)
	}
	if !strings.Contains(prov.lastReq.System, "Household context") {
		t.Errorf("system prompt missing household context: %q", prov.lastReq.System)
	}
	if !strings.Contains(prov.lastReq.System, `"household_id":7`) {
		t.Errorf("system prompt didn't embed the context JSON: %q", prov.lastReq.System)
	}

	// Persisted user message must carry the user_id attribution.
	all, _ := msgs.ListByThread(context.Background(), thr.ID)
	if len(all) != 2 {
		t.Fatalf("messages persisted = %d, want 2", len(all))
	}
	if all[0].UserID == nil || *all[0].UserID != 42 {
		t.Errorf("user message UserID = %v, want 42 (shared-thread attribution)", all[0].UserID)
	}
	if all[1].UserID != nil {
		t.Errorf("assistant message UserID = %v, want nil", all[1].UserID)
	}
}

// TestSendSharedMessage_NonMemberRejected: a non-member can't post even
// if they guess the thread id.
func TestSendSharedMessage_NonMemberRejected(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	hh := &stubHouseholdAccess{}
	hh.setActive(42, 7, true) // only userA is active
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(&stubProvider{deltas: []ai.Delta{{Done: true}}})).
		WithHouseholdAccess(hh)

	thr, _ := svc.CreateSharedThread(context.Background(), 42, 7, nil)
	// userB (not a member) tries to post.
	_, err := svc.SendSharedMessage(context.Background(), 99, 7, thr.ID, "leak?")
	if !errors.Is(err, ai.ErrNotHouseholdMember) {
		t.Fatalf("err = %v, want ErrNotHouseholdMember", err)
	}
}

// TestListSharedThreads_MemberSeesShared: two members both see the
// shared thread; a non-member sees nothing.
func TestListSharedThreads_MemberSeesShared(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	hh := &stubHouseholdAccess{}
	hh.setActive(1, 7, true)
	hh.setActive(2, 7, true)
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(&stubProvider{})).
		WithHouseholdAccess(hh)

	if _, err := svc.CreateSharedThread(context.Background(), 1, 7, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Member 2 sees the thread.
	listB, err := svc.ListSharedThreads(context.Background(), 2, 7)
	if err != nil {
		t.Fatalf("ListSharedThreads(2): %v", err)
	}
	if len(listB) != 1 {
		t.Errorf("member 2 saw %d shared threads, want 1", len(listB))
	}

	// Non-member 99 is rejected.
	if _, err := svc.ListSharedThreads(context.Background(), 99, 7); !errors.Is(err, ai.ErrNotHouseholdMember) {
		t.Errorf("non-member err = %v, want ErrNotHouseholdMember", err)
	}
}

// TestGetSharedThread_PersonalReachable: even on the /h/ai surface, a
// user's own personal thread is reachable (the access method allows owner
// OR shared+member). The handler should still reject because the
// SendSharedMessage path checks `SharedWithHousehold` separately, but the
// repo-level access is permissive.
func TestGetSharedThread_PersonalReachable(t *testing.T) {
	threads := newStubThreadRepo()
	msgs := newStubMessageRepo()
	hh := &stubHouseholdAccess{}
	hh.setActive(42, 7, true)
	svc := ai.NewService(threads, msgs, nil, ai.StaticResolver(&stubProvider{})).
		WithHouseholdAccess(hh)

	personal, _ := svc.CreateThread(context.Background(), 42, nil)
	got, err := svc.GetSharedThread(context.Background(), 42, 7, personal.ID)
	if err != nil {
		t.Fatalf("GetSharedThread for personal thread: %v", err)
	}
	if got.ID != personal.ID {
		t.Errorf("got = %+v, want personal thread", got)
	}
}
