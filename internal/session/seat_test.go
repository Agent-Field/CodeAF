package session

import (
	"context"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	procexec "github.com/Agent-Field/codeaf/internal/executor"
)

// countingSeat is a seat that remembers the directories it was asked for and
// the tool calls that went around it.
type countingSeat struct {
	procexec.Stance
	mu    sync.Mutex
	dirs  []string
	tools []string
}

func (s *countingSeat) In(dir string) procexec.Runner {
	s.mu.Lock()
	s.dirs = append(s.dirs, dir)
	s.mu.Unlock()
	return s.Stance.In(dir)
}

func (s *countingSeat) Around(_ context.Context, call procexec.Call, run func() ([]byte, bool)) error {
	s.mu.Lock()
	s.tools = append(s.tools, call.Tool)
	s.mu.Unlock()
	run()
	return nil
}

// A session's tool calls run on the seat the session was given, not on a lookup
// by path: two shell calls go around that seat twice, and their processes are
// rooted in the workspace.
func TestToolCallsRunOnTheSessionSeat(t *testing.T) {
	seat := &countingSeat{}
	completer := &routedCompleter{parent: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("a", "bash", `{"command":"echo one > one.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("b", "bash", `{"command":"echo two > two.txt"}`), nil
		},
		finalText("done"),
	}}
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = workspace
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
		config.Seat = seat
	})
	collect(t, mustSubmit(t, agent, "go"))

	seat.mu.Lock()
	defer seat.mu.Unlock()
	if len(seat.tools) != 2 || seat.tools[0] != "bash" {
		t.Fatalf("tool calls that went around the session seat = %v, want two bash calls", seat.tools)
	}
	if len(seat.dirs) == 0 {
		t.Fatal("the shell never asked the session seat for a runner")
	}
	for _, dir := range seat.dirs {
		if dir != workspace {
			t.Errorf("a call was rooted at %q, want the workspace %q", dir, workspace)
		}
	}
}
