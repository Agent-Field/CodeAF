package tui3

import (
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestReturningTabKeepsItsTextWhileHostedReplayIsPending(t *testing.T) {
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	first := &pendingTabReplayAgent{atomicBoundaryAgent: replayTestAgent(nil), release: release}
	first.entries = []session.DisplayEntry{{Role: "user", Text: "first tab request"}, {Role: "assistant", Text: "first tab answer", Answer: true}}
	a := newTestApp(first)
	a.file = t.TempDir() + "/first/transcript.jsonl"
	file := a.file
	a.replayList(first.entries)
	a.welcome = welcome{spent: true}
	a.frame()
	spend(t, a, a.takeBeside(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: t.TempDir() + "/second/transcript.jsonl"}))
	cmd, ours := a.bringForward(file)
	if !ours {
		t.Fatal("first tab was not kept")
	}
	before := plain(mustFrame(a))
	// The door line starts reading before its returned command is driven.
	// Hold that read until the immediate frame and the hidden update are ready.
	first.entries = append(first.entries, session.DisplayEntry{Role: "assistant", Text: "new text arrived behind the tab", Answer: true})
	unblock()
	spend(t, a, cmd)
	after := plain(mustFrame(a))
	if !strings.Contains(before, "first tab answer") {
		t.Fatalf("tab went blank while its replay was pending:\n%s", before)
	}
	if strings.Count(after, "first tab answer") != 1 || !strings.Contains(after, "new text arrived behind the tab") {
		t.Fatalf("replay duplicated stale text or lost the update:\n%s", after)
	}
}

// pendingTabReplayAgent holds the hosted read while the cached tab is drawn.
type pendingTabReplayAgent struct {
	*atomicBoundaryAgent
	release <-chan struct{}
}

func (a *pendingTabReplayAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	<-a.release
	return a.atomicBoundaryAgent.AttachReplay()
}
