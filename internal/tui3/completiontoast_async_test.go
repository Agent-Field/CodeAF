package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func asyncCompletionView(t *testing.T) (*app, []tea.Msg, string) {
	t.Helper()
	target := t.TempDir() + "/owner/transcript.jsonl"
	host := replayTestAgent(nil)
	for i := 0; i < 24; i++ {
		host.entries = append(host.entries, session.DisplayEntry{Role: "user", Text: "Recorded request " + itoa(i)}, session.DisplayEntry{Role: "assistant", Text: "Recorded answer " + itoa(i), Answer: true})
	}
	a := toastFixture()
	a.width, a.height = 120, 24
	a.behind = map[string]*kept{a.convKey(target): {conv: Conversation{Agent: host, SessionFile: target}, side: &aside{}}}
	a.queueCompletionToast(target, 0, 9, 0, false)
	cmd := a.viewCompletionToast()
	if a.file != target || !a.hostReplayLoading || a.pendingCompletionReveal == nil {
		t.Fatal("View did not retain navigation while hosted journal loads")
	}
	// Execute the real AttachReplay command but deliberately hold its UI fold.
	messages := runCmd(cmd)
	if !a.hostReplayLoading || a.pendingCompletionReveal == nil {
		t.Fatal("request disappeared before asynchronous journal fold")
	}
	return a, messages, target
}

func TestCompletionToastViewSurvivesAsyncJournalThenRoster(t *testing.T) {
	a, messages, target := asyncCompletionView(t)
	drive(t, a, messages...)
	if a.file != target || a.hostReplayLoading {
		t.Fatal("hosted replay did not finish in owning conversation")
	}
	if a.pendingCompletionReveal == nil {
		t.Fatal("View was discarded before delayed task roster arrived")
	}
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(9, "Delayed restored task", session.TaskDone, session.TaskNotice{Result: "Retained result"})})
	if a.pendingCompletionReveal != nil {
		t.Fatal("View intent remained pending after matching work arrived")
	}
	found := false
	for _, r := range a.visible(a.bodyWidth()) {
		if r.hit == hitWorkCompletion && r.completion.task == 9 {
			found = true
		}
	}
	if !found {
		t.Fatal("matching delayed result did not open in its disclosure")
	}
	if a.offset <= 0 {
		t.Fatal("View did not scroll to delayed work below hosted journal")
	}
	// Later unrelated frames must not replay the explicit navigation after success.
	a.offset, a.stick = 0, false
	drive(t, a, frameMsg{})
	if a.offset != 0 || a.pendingCompletionReveal != nil {
		t.Fatal("later frame stole scrolling after successful View")
	}
}

func TestCompletionToastAsyncViewCancelsWhenUserChangesConversation(t *testing.T) {
	a, messages, _ := asyncCompletionView(t)
	a.detachConversation()
	other := t.TempDir() + "/other/transcript.jsonl"
	drive(t, a, runCmd(a.attachConversation(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: other}, nil))...)
	drive(t, a, messages...)
	if a.file != other || a.pendingCompletionReveal != nil {
		t.Fatal("old View intent survived navigation to another conversation")
	}
	a.offset, a.stick = 0, false
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(9, "Different chat same task number", session.TaskDone, session.TaskNotice{})})
	if a.offset != 0 || a.pendingCompletionReveal != nil {
		t.Fatal("old task identity changed the new conversation's scroll")
	}
}
