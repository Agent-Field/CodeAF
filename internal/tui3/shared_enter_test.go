package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// unfold runs one command the way the runtime would: batches expand, every
// leaf runs, and what the leaves say goes back through the loop.
func unfold(t *testing.T, a *app, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	got := cmd()
	switch batch := got.(type) {
	case tea.BatchMsg:
		for _, child := range batch {
			unfold(t, a, child)
		}
	default:
		if got != nil {
			a.Update(got)
		}
	}
}

// THE SHARED ROAD STAYS LIVE (#1659). A shared legacy connection used to run
// the whole launch assembly inside the enter keystroke — subharness wiring,
// the memory graph, the foreign-skill pass — and the update loop froze for as
// long as the engine took. The door is off the loop now, on the door line
// every other door uses; only the commit stays on it. This test holds the
// door shut and requires the keystroke to come back anyway.
func TestTheSharedHomeEnterKeepsTheLoopLive(t *testing.T) {
	l := newHomeLab(t)
	a := l.app("")
	a.shared = true
	// A LIVE DOOR LINE, like every test that asks a door off-loop: without one
	// the fold never comes back and the test hangs instead of failing.
	a.doorLine = newDoorLine()
	t.Cleanup(a.doorLine.close)
	release := make(chan struct{})
	a.start = func(workspace string) (Conversation, error) {
		<-release
		return Conversation{
			Agent:       &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
			SessionFile: filepath.Join(workspace, "next", "transcript.jsonl"),
			Workspace:   workspace,
		}, nil
	}
	done := make(chan tea.Cmd, 1)
	go func() { done <- a.homeStartWithProject("hello", "") }()
	// ONE RECEIVE, KEPT: the buffered value the select consumes is the very
	// command the fold rides — receiving twice would wait for a second open
	// that never comes.
	var cmd tea.Cmd
	select {
	case cmd = <-done:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("the shared road ran the launch assembly inside the keystroke: homeStartWithProject did not come back while the door was out")
	}
	// THE DOOR IS OUT AND THE LOOP IS LIVE: a frame-sized message is handled
	// while the engine is still thinking.
	drive(t, a, tea.WindowSizeMsg{Width: 80, Height: 24})
	// THE COMMIT LANDS ON THE LOOP: let the door finish and carry the fold
	// back through the runtime, and the swap happens where the synchronous
	// road put it — after the door, before anything else draws.
	close(release)
	if cmd == nil {
		t.Fatal("the shared road's command carried no fold")
	}
	unfold(t, a, cmd)
	if a.file == "" || !strings.HasSuffix(a.file, "transcript.jsonl") {
		t.Fatalf("the fold did not commit the door's conversation; file is %q", a.file)
	}
}

// AND THE REFUSAL COSTS NOTHING. The synchronous road closed home before it
// knew the door's answer, because the answer came inside the keystroke; the
// async road knows later, so the closing steps ride the fold — a failed door
// swaps nothing and leaves the surface exactly where the person typed.
func TestASharedHomeRefusalKeepsHomeOpen(t *testing.T) {
	l := newHomeLab(t)
	a := l.app("")
	a.shared = true
	a.doorLine = newDoorLine()
	t.Cleanup(a.doorLine.close)
	a.start = func(workspace string) (Conversation, error) {
		return Conversation{}, errTestDoorRefused
	}
	done := make(chan tea.Cmd, 1)
	go func() { done <- a.homeStartWithProject("hello", "") }()
	var cmd tea.Cmd
	select {
	case cmd = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the shared road blocked on a refusing door")
	}
	if cmd == nil {
		t.Fatal("the refused door carried no fold")
	}
	unfold(t, a, cmd)
	if a.file != "" {
		t.Fatalf("a refused door still committed a conversation: %q", a.file)
	}
	if a.conversationOpening {
		t.Fatal("a refused door left the opening guard standing")
	}
}

// errTestDoorRefused is the shared test doors' refusal.
var errTestDoorRefused = errTestRefused{}

type errTestRefused struct{}

func (errTestRefused) Error() string { return "the test door refused" }
