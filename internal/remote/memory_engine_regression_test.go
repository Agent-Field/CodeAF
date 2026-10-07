package remote

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// The ordinary launch holds a remote.Agent, so the command contract must be
// exercised through that handle and a real session and store at the far end.
func TestDefaultEngineMemoryCommandsReachTheRealSession(t *testing.T) {
	brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	seeded, err := brain.AddMemory(store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser,
		Title: "seasonal amber fixture", Text: "seasonal amber fixture"})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce sync.Once
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		}
		enteredOnce.Do(func() { close(entered) })
		<-release
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"op\\\":\\\"add\\\"}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"op\":\"add\"}"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(model.Close)
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/memory-wire",
		APIKey: "fixture", BaseURL: model.URL + "/v1", System: "Test only.", PromptProfile: "full", Memory: brain})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	saved := make(chan struct{})
	recorder := &memorySaveRecorder{Agent: far, saved: saved}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: recorder, Memory: engineMemoryRows{brain}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	commands, ok := any(loop.Client.Agent()).(MemoryCommands)
	if !ok || !commands.Remembers() {
		t.Fatal("the ordinary engine handle says enabled memory is absent")
	}
	lines, err := commands.Memories("")
	if err != nil || len(lines) != 1 || lines[0].ID != seeded.ID {
		t.Fatalf("seeded memories = %+v, %v", lines, err)
	}
	title, err := commands.Remember("prefers tabs over spaces in Go")
	if err != nil || title == "" {
		t.Fatalf("remember = %q, %v", title, err)
	}
	lines, err = commands.Memories("tabs")
	if err != nil || len(lines) != 1 || lines[0].Text != "prefers tabs over spaces in Go" {
		t.Fatalf("newly saved memory = %+v, %v", lines, err)
	}
	shelves, err := loop.Client.Snapshot(500)
	if err != nil || shelves.Held != 2 {
		t.Fatalf("the memory place snapshot = %+v, %v", shelves, err)
	}
	forgot, err := commands.Forget("tabs")
	if err != nil || forgot != title {
		t.Fatalf("forget = %q, %v; want %q", forgot, err, title)
	}
	lines, err = commands.Memories("tabs")
	if err != nil || len(lines) != 0 {
		t.Fatalf("forgotten memory still listed = %+v, %v", lines, err)
	}

	// The decider is held by a channel, not a sleep. A receipt that expires
	// while that real save runs must stay uncertain, and the write still lands.
	_, answered, err := loop.Client.callAnswered(nil, MethodMemoryRemember, "seasonal amber fixture stays", 20*time.Millisecond)
	if answered || !errors.Is(err, ErrLate) {
		t.Fatalf("held save receipt = answered %v, %v", answered, err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the real memory decider was never reached")
	}
	close(release)
	select {
	case <-saved:
	case <-time.After(10 * time.Second):
		t.Fatal("the real save did not finish after its late receipt")
	}
	lines, err = commands.Memories("stays")
	if err != nil || len(lines) != 1 || lines[0].Text != "seasonal amber fixture stays" {
		t.Fatalf("the late save did not land: %+v, %v", lines, err)
	}
}

func TestDefaultEngineMemoryOffIsNotAnEmptyEnabledStore(t *testing.T) {
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/memory-off",
		APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only.", PromptProfile: "full"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	commands, ok := any(loop.Client.Agent()).(MemoryCommands)
	if !ok || commands.Remembers() {
		t.Fatal("the ordinary engine handle does not distinguish memory off")
	}
	for _, method := range []string{MethodMemoryRemember, MethodMemoryForgetQuery, MethodMemoryMemories} {
		if _, err := loop.Client.call(nil, method, "tabs"); err == nil || !strings.Contains(err.Error(), memoryOffWord) {
			t.Fatalf("%s with memory off = %v", method, err)
		}
	}
	if _, err := loop.Client.Snapshot(500); err == nil || !strings.Contains(err.Error(), memoryOffWord) {
		t.Fatalf("snapshot with memory off = %v", err)
	}
}

// The test uses the store's own rows for the place, the same translation the
// executable's v3Brain supplies, so command writes and page reads share a brain.
// ListMemories spells the scope words the wire carries, the way the executable's
// door does.
type engineMemoryRows struct{ *store.Store }

func (m engineMemoryRows) Snapshot(limit int) (store.MemoryShelves, error) {
	return m.MemorySnapshot(limit)
}

func (m engineMemoryRows) ChangedSince(at time.Time) (int, int, error) {
	return m.MemoryChangedSince(at)
}

func (m engineMemoryRows) ListMemories(scope string, limit int) ([]store.Memory, error) {
	switch strings.TrimSpace(scope) {
	case "":
		return m.Store.ListMemories(nil, limit)
	case store.MemoryScopeUser:
		return m.Store.ListMemories([]string{store.OwnerUser}, limit)
	case store.MemoryScopeEnv:
		return m.Store.ListMemories([]string{store.OwnerMachine}, limit)
	case store.MemoryScopeProject:
		// THE SAME ANSWER THE EXECUTABLE'S DOOR GIVES: every project's rows,
		// the quarantine included ([store.Store.ListProjectMemories]).
		return m.Store.ListProjectMemories(limit)
	}
	return nil, nil
}

// The recorder leaves the real memory implementation intact and only exposes
// its completion, so the test never polls a store racing its writer.
type memorySaveRecorder struct {
	*session.Agent
	saved chan struct{}
}

func (a *memorySaveRecorder) Remember(text string) (string, error) {
	title, err := a.Agent.Remember(text)
	if text == "seasonal amber fixture stays" {
		close(a.saved)
	}
	return title, err
}
