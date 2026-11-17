package knowledge_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// liveHistory is one chat that is still being written in: its journal's
// modification time is whatever the test last said.
type liveHistory struct {
	mu       sync.Mutex
	journals []knowledge.Journal
}

func (h *liveHistory) set(journals ...knowledge.Journal) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.journals = journals
}

func (h *liveHistory) Inventory(context.Context) ([]knowledge.Journal, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]knowledge.Journal(nil), h.journals...), nil
}

func (h *liveHistory) Read(_ context.Context, j knowledge.Journal, _ knowledge.JournalCursor) (knowledge.JournalBatch, error) {
	return knowledge.JournalBatch{Sources: []knowledge.Source{{ID: j.ID, Session: j.Session, Role: "user", TS: "2026-11-16T22:29:00Z", Text: "Cedar queues jobs."}}, Cursor: knowledge.JournalCursor{Offset: j.Size}}, nil
}

// A CHAT BEING WRITTEN IN IS NOT COMPILED UNTIL IT GOES QUIET, AND THEN ITS
// CALLS ARE CHARGED TO IT. Live lane E6b paid $0.654 of a $0.96 session for 175
// Library calls in 42 passes: the build loop compiled every chat in two-second
// slices while it was still being typed in, kept going after the last request,
// and charged none of it to any conversation (build_idle.go).
func TestALiveChatIsCompiledOnceItGoesQuietAndChargedToIt(t *testing.T) {
	var clock atomic.Int64
	start := time.Date(2026, 11, 16, 22, 29, 0, 0, time.UTC)
	clock.Store(start.UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	knowledge.SetBuildTimingForTest(t, 5*time.Millisecond, 20*time.Millisecond, 2*time.Minute, now)

	v := knowledge.Open(libraryAccountingProfile(t))
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["messages"] != nil {
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": "live", "quote": "Cedar queues jobs."}}}}}
		}
		return map[string]any{"subjects": []any{}}
	})
	fixture := http.DefaultTransport
	var calls atomic.Int64
	http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		response, err := fixture.RoundTrip(r)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			return nil, err
		}
		body["usage"] = map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "cost": .01}
		raw, err := json.Marshal(body)
		response.Body = io.NopCloser(strings.NewReader(string(raw)))
		return response, err
	})
	var chargedMu sync.Mutex
	var charged []string
	knowledge.SetUsageRecorder(func(ctx context.Context, _ provider.Billed) {
		chargedMu.Lock()
		defer chargedMu.Unlock()
		charged = append(charged, knowledge.ChargedSession(ctx))
	})
	t.Cleanup(func() { knowledge.SetUsageRecorder(nil) })

	history := &liveHistory{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- v.Run(ctx, history, func() string { return "fixture" }) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("worker did not stop")
		}
	}()
	// The Build a person pressed finds nothing yet, which ends its backfill;
	// what follows is background work deciding for itself.
	waitPhase(t, v, knowledge.BuildWaiting)

	// The chat is written in, and keeps being written in for a minute.
	for minute := 0; minute <= 60; minute += 15 {
		at := start.Add(time.Duration(minute) * time.Second)
		clock.Store(at.UnixNano())
		history.set(knowledge.Journal{ID: "live", Session: "chat-7", Size: int64(minute + 1), Modified: at})
		time.Sleep(60 * time.Millisecond)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the Library made %d model calls while the chat was still being written in; want none until it goes quiet", n)
	}

	// Two minutes after its last write, the chat is compiled — once.
	clock.Store(start.Add(60*time.Second + 2*time.Minute).UnixNano())
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hits, err := v.Search("queues", 5); err == nil && len(hits) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hits, err := v.Search("queues", 5); err != nil || len(hits) != 1 {
		t.Fatalf("the chat was not compiled after it went quiet: %+v %v", hits, err)
	}
	if calls.Load() == 0 {
		t.Fatal("no model call compiled the quiet chat")
	}
	chargedMu.Lock()
	defer chargedMu.Unlock()
	for _, session := range charged {
		if session != "chat-7" {
			t.Fatalf("a Library call was charged to %q, want the chat it compiled (chat-7); all: %v", session, charged)
		}
	}
	if len(charged) == 0 {
		t.Fatal("no Library call reached the spending ledger")
	}
}

func waitPhase(t *testing.T, v *knowledge.Vault, want knowledge.BuildPhase) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Build.Phase == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("build never reached %q", want)
}
