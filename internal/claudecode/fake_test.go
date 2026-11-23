package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// TestMain lets this test binary stand in for Claude Code: started with
// FAKE_CLAUDE set, it speaks the stream-json control protocol the way the CLI
// does — a handshake, one turn per message with one shell call that it asks
// permission for, and an exit when its input closes — so the whole run can be
// held without a network, a key or a plan.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CLAUDE") != "" {
		fakeClaude()
		return
	}
	os.Exit(m.Run())
}

func fakeClaude() {
	out := json.NewEncoder(os.Stdout)
	in := bufio.NewScanner(os.Stdin)
	answers := make(chan map[string]any, 4)
	users := make(chan string, 8)
	go func() {
		for in.Scan() {
			var line map[string]any
			_ = json.Unmarshal(in.Bytes(), &line)
			switch line["type"] {
			case "control_request":
				out.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": line["request_id"], "response": map[string]any{}}})
			case "control_response":
				answers <- line["response"].(map[string]any)
			case "user":
				content := line["message"].(map[string]any)["content"].([]any)
				users <- content[0].(map[string]any)["text"].(string)
			}
		}
		close(users)
	}()
	out.Encode(map[string]any{"type": "system", "subtype": "init", "model": "claude-fake", "apiKeySource": "none", "session_id": "fake-session"})
	turn := 0
	for text := range users {
		turn++
		command := "go test ./..."
		if strings.Contains(text, "push") {
			command = "git push origin HEAD"
		}
		id := fmt.Sprintf("t%d", turn)
		out.Encode(map[string]any{"type": "assistant", "session_id": "fake-session", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": command}}}}})
		out.Encode(map[string]any{"type": "control_request", "request_id": "p" + id, "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": command}, "tool_use_id": id}})
		answer := <-answers
		verdict := answer["response"].(map[string]any)["behavior"]
		out.Encode(map[string]any{"type": "user", "session_id": "fake-session", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": fmt.Sprint(verdict)}}}})
		// FOLDED: a message that arrived during the turn is taken into it,
		// and the turn ends with one result for both, as the real CLI does.
		if os.Getenv("FAKE_CLAUDE") == "fold" {
			time.Sleep(time.Second)
			for drained := false; !drained; {
				select {
				case more, ok := <-users:
					if ok {
						text += " + " + more
					}
				default:
					drained = true
				}
			}
		}
		out.Encode(map[string]any{"type": "result", "subtype": "success", "session_id": "fake-session", "result": "did: " + text, "total_cost_usd": 0.01 * float64(turn), "num_turns": turn})
	}
}

// listeningFake is a host a person can talk to while the run works.
type listeningFake struct {
	mu      sync.Mutex
	stages  []delegate.StageRecord
	steps   []delegate.StepRecord
	ending  *delegate.Ending
	queued  []delegate.Message
	heard   []string
	closed  string
	onStep  func()
	stepped bool
}

func (h *listeningFake) Workspace() string           { return "." }
func (h *listeningFake) Ceilings() delegate.Ceilings { return delegate.Ceilings{Hours: 0.05} }
func (h *listeningFake) Hello([]string)              {}
func (h *listeningFake) Models() delegate.ModelAPI   { return delegate.ModelAPI{} }
func (h *listeningFake) Stage(s delegate.StageRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stages = append(h.stages, s)
}
func (h *listeningFake) Step(s delegate.StepRecord) {
	h.mu.Lock()
	h.steps = append(h.steps, s)
	first := !h.stepped
	h.stepped = true
	h.mu.Unlock()
	if first && h.onStep != nil {
		h.onStep()
	}
}
func (h *listeningFake) Terminal(e delegate.Ending) { h.mu.Lock(); h.ending = &e; h.mu.Unlock() }
func (h *listeningFake) Messages() []delegate.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.queued
	h.queued = nil
	return out
}
func (h *listeningFake) Heard(ids []string) {
	h.mu.Lock()
	h.heard = append(h.heard, ids...)
	h.mu.Unlock()
}
func (h *listeningFake) CloseInbox(reason string) { h.mu.Lock(); h.closed = reason; h.mu.Unlock() }

func fakeBinary(t *testing.T) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvBinary, self)
	t.Setenv("FAKE_CLAUDE", "1")
	previous := idleGrace
	idleGrace = 300 * time.Millisecond
	t.Cleanup(func() { idleGrace = previous })
}

func TestAMessageFoldedIntoTheRunningTurnStillLetsTheRunEnd(t *testing.T) {
	fakeBinary(t)
	t.Setenv("FAKE_CLAUDE", "fold")
	host := &listeningFake{}
	host.onStep = func() {
		host.mu.Lock()
		host.queued = append(host.queued, delegate.Message{ID: "m1", From: delegate.FromPerson, Text: "also add Sub"})
		host.mu.Unlock()
	}
	done := make(chan struct{})
	go func() { run(context.Background(), host, "add Add", "", ""); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("one result for two messages left the run waiting forever")
	}
	if host.ending == nil || host.ending.Deliverable != "did: add Add + also add Sub" {
		t.Fatalf("ending = %+v", host.ending)
	}
}

func TestAMessageSentWhileItWorksIsTakenInBeforeTheRunEnds(t *testing.T) {
	fakeBinary(t)
	host := &listeningFake{}
	host.onStep = func() {
		host.mu.Lock()
		host.queued = append(host.queued, delegate.Message{ID: "m1", From: delegate.FromPerson, Text: "also add Sub"})
		host.mu.Unlock()
	}
	done := make(chan struct{})
	go func() { run(context.Background(), host, "add Add", "", ""); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run never ended")
	}
	if host.ending == nil || host.ending.Status != delegate.StatusPass {
		t.Fatalf("ending = %+v", host.ending)
	}
	if host.ending.Deliverable != "did: also add Sub" {
		t.Fatalf("the last turn answered %q, not the person's message", host.ending.Deliverable)
	}
	if len(host.heard) != 1 || host.heard[0] != "m1" || host.closed == "" {
		t.Fatalf("heard %v, inbox closed %q", host.heard, host.closed)
	}
	if host.ending.Extra["claude_session"] != "fake-session" || host.ending.CostUSD != 0 {
		t.Fatalf("extra %v cost %v", host.ending.Extra, host.ending.CostUSD)
	}
}

func TestAPushIsRefusedOnTheWireAndTheRunStillEnds(t *testing.T) {
	fakeBinary(t)
	host := &listeningFake{}
	done := make(chan struct{})
	go func() { run(context.Background(), host, "push it", "", ""); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run never ended")
	}
	var refused bool
	for _, s := range host.steps {
		refused = refused || strings.HasPrefix(s.Command, "refused: git push")
	}
	if !refused || host.ending == nil {
		t.Fatalf("steps %+v ending %+v", host.steps, host.ending)
	}
}

func TestStoppingTheTaskInterruptsAndEnds(t *testing.T) {
	fakeBinary(t)
	host := &listeningFake{}
	ctx, cancel := context.WithCancel(context.Background())
	host.onStep = cancel
	done := make(chan struct{})
	go func() { run(ctx, host, "add Add", "", ""); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a stopped run never ended")
	}
	if host.ending == nil || host.ending.Message != "Claude Code was stopped" {
		t.Fatalf("ending = %+v", host.ending)
	}
}
