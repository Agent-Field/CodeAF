package tui3

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// Only the waiting boundary is held; the wire, session and store are real.
// This reproduces a slow engine without a sleep or a provider request.
type heldEngineMemory struct {
	*session.Agent
	entered, release chan struct{}
	once             sync.Once
}

func (h *heldEngineMemory) wait() { h.once.Do(func() { close(h.entered) }); <-h.release }
func (h *heldEngineMemory) Remember(text string) (string, error) {
	h.wait()
	return h.Agent.Remember(text)
}
func (h *heldEngineMemory) Forget(query string) (string, error) {
	h.wait()
	return h.Agent.Forget(query)
}
func (h *heldEngineMemory) Memories(query string) ([]session.MemoryLine, error) {
	h.wait()
	return h.Agent.Memories(query)
}

func TestMemoryWireWaitLeavesTypingAndRepaintAvailable(t *testing.T) {
	for _, tc := range []struct{ command, receipt string }{
		{"/remember cobalt shipment fixture", "remembered ·"},
		{"/forget neovim", "forgot ·"},
		{"/memories neovim", "neovim"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = brain.Close() })
			_, err = brain.AddMemory(store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeUser, Title: "editor", Text: "favorite editor is neovim"})
			if err != nil {
				t.Fatal(err)
			}
			far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/memory", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only.", PromptProfile: "full", Memory: brain})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = far.Close() })
			held := &heldEngineMemory{Agent: far, entered: make(chan struct{}), release: make(chan struct{})}
			loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: held}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loop.Close() })
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(held.release) }) }
			t.Cleanup(release)
			a := newTestApp(loop.Client.Agent())
			a.input.setText(tc.command)
			returned := make(chan tea.Cmd, 1)
			go func() { _, cmd := a.Update(key("enter")); returned <- cmd }()
			var cmd tea.Cmd
			select {
			case cmd = <-returned:
			case <-time.After(time.Second):
				release()
				<-returned
				t.Fatal("Update waited for the memory engine")
			}
			if cmd == nil {
				t.Fatal("memory command did not schedule its engine call")
			}
			replies := make(chan tea.Msg, 32)
			// Update batches command work with ordinary surface bookkeeping.
			// Run those commands concurrently, as Bubble Tea does, and retain
			// only this operation's receipt rather than following paint clocks.
			var launch func(tea.Cmd)
			launch = func(command tea.Cmd) {
				if command == nil {
					return
				}
				go func() {
					msg := command()
					if batch, ok := msg.(tea.BatchMsg); ok {
						for _, child := range batch {
							launch(child)
						}
					} else {
						replies <- msg
					}
				}()
			}
			launch(cmd)
			select {
			case <-held.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("real engine memory door was not reached")
			}
			a.Update(key("x"))
			if a.input.String() != "x" || !strings.Contains(plain(frame(a)), "x") {
				t.Fatal("typing or repaint stopped while the engine waited")
			}
			release()
			deadline := time.After(5 * time.Second)
			for !noted(a, tc.receipt) {
				select {
				case msg := <-replies:
					a.Update(msg)
				case <-deadline:
					t.Fatal("memory receipt did not arrive")
				}
			}
			if got := plain(lastNote(t, a)); !strings.Contains(got, tc.receipt) {
				t.Fatalf("memory receipt=%q", got)
			}
		})
	}
}

func TestMemoryReceiptReturnsOnlyToItsOriginatingConversation(t *testing.T) {
	original := &rememberingAgent{}
	a := newTestApp(original)
	a.file = "/tmp/lab/original.jsonl"
	cmd := a.slash("/remember cobalt shipment fixture")
	if cmd == nil {
		t.Fatal("memory command has no deferred receipt")
	}
	conv := a.front()
	held := &kept{conv: conv, side: &aside{}}
	a.behind = map[string]*kept{"original": held}
	a.agent = &fakeAgent{model: "m"}
	a.file = "/tmp/lab/other.jsonl"
	a.entries = nil
	reply := cmd()
	a.Update(reply)
	if len(a.entries) != 0 {
		t.Fatal("late memory receipt appeared in another conversation")
	}
	if len(held.side.parkNotes) != 1 || !strings.Contains(held.side.parkNotes[0], "remembered ·") {
		t.Fatalf("original receipt=%q", held.side.parkNotes)
	}
	// A remote handle can survive a session swap, so pointer equality alone
	// must not deliver the old conversation's receipt into its new transcript.
	a.agent = original
	a.Update(reply)
	if len(a.entries) != 0 {
		t.Fatal("shared agent handle routed receipt to a different transcript")
	}
	delete(a.behind, "original")
	a.Update(reply)
	if len(a.entries) != 0 {
		t.Fatal("closed conversation receipt appeared elsewhere")
	}
	// The real attachment path consumes the held receipts when their original
	// conversation returns, rather than merely retaining an unreachable line.
	a.attachConversation(conv, held.side)
	if got := plain(lastNote(t, a)); !strings.Contains(got, "remembered ·") {
		t.Fatalf("returning to the original conversation lost its receipt: %q", got)
	}
}
