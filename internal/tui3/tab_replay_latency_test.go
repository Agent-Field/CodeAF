package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestReturningTabKeepsItsTextWhileHostedReplayIsPending(t *testing.T) {
	first := replayTestAgent(nil)
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
	// Drive the authoritative replay after observing the immediate switch frame.
	first.entries = append(first.entries, session.DisplayEntry{Role: "assistant", Text: "new text arrived behind the tab", Answer: true})
	spend(t, a, cmd)
	after := plain(mustFrame(a))
	if !strings.Contains(before, "first tab answer") {
		t.Fatalf("tab went blank while its replay was pending:\n%s", before)
	}
	if strings.Count(after, "first tab answer") != 1 || !strings.Contains(after, "new text arrived behind the tab") {
		t.Fatalf("replay duplicated stale text or lost the update:\n%s", after)
	}
}
