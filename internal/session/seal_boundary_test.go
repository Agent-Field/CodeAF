package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// transcriptAtSeal is a store that keeps the transcript exactly as each seal
// found it, which is all a machine that picks the chat up from that seal gets.
type transcriptAtSeal struct {
	path  string
	mu    sync.Mutex
	taken []string
}

func (s *transcriptAtSeal) Seal(_ context.Context, _ cell.Cell, _ cellstore.TurnInfo) (cellstore.Sealed, error) {
	raw, _ := os.ReadFile(s.path)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taken = append(s.taken, string(raw))
	return cellstore.Sealed{}, nil
}

func (s *transcriptAtSeal) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.taken) == 0 {
		return ""
	}
	return s.taken[len(s.taken)-1]
}

func (s *transcriptAtSeal) seals() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.taken)
}

// The seal of a tool call holds the call's result line, and the seal of a turn
// holds its closing answer: a process killed right after either leaves the
// other machine a transcript that says what the work was and what came of it,
// so the model does not run a finished call again.
func TestSealsCarryTheToolResultAndTheClosingAnswer(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: c.Root}
	store := &transcriptAtSeal{path: place.Transcript()}
	var told int
	seat, err := cellstore.SeatOver(executor.HostBound, c, t.TempDir(), nil, nil, func(cellstore.Engine) cellstore.Store { return store })
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		toolStep("read", `{"path":"a.txt"}`),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the closing answer"), nil
		},
	}}, func(cfg *Config) {
		cfg.Seat = seat
		cfg.SessionFile = place.Transcript()
		cfg.Place = Place{Dir: c.Root, Workspace: cfg.Workspace}
		cfg.Seals = turnCounter{&told}
		must(t, os.WriteFile(filepath.Join(cfg.Workspace, "a.txt"), []byte("alpha-contents"), 0o600))
	})
	collect(t, mustSubmit(t, agent, "read it"))

	if got := store.last(); !strings.Contains(got, "alpha-contents") || !strings.Contains(got, "the closing answer") {
		t.Fatalf("the last seal (of %d) holds a transcript without the result or the answer:\n%s", store.seals(), got)
	}
	if !sealHolds(store, "alpha-contents") {
		t.Fatal("no seal before the closing answer holds the tool result")
	}
	if told == 0 {
		t.Fatal("the sync side was never told the turn ended")
	}
}

// sealHolds reports whether the first seal that holds the result is one taken
// before the closing answer was written: the call's own seal.
func sealHolds(s *transcriptAtSeal, result string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, taken := range s.taken {
		if strings.Contains(taken, result) && !strings.Contains(taken, "the closing answer") {
			return true
		}
	}
	return false
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// turnCounter is the seal state of a session that only counts turn ends.
type turnCounter struct{ n *int }

func (turnCounter) Failing() bool { return false }
func (turnCounter) Take() string  { return "" }
func (c turnCounter) TurnEnded()  { *c.n++ }
