package tui3

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Contract E3 keeps opening words and an unfinished paste across an orderly exit.
func TestSignalQuitRestoresTheOpeningDraftOnNextStart(t *testing.T) {
	a := openingLab(t)
	a.closeHome()
	a.draftFile = filepath.Join(t.TempDir(), "draft.txt")
	release := make(chan struct{})
	a.start = func(string) (Conversation, error) {
		<-release
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/next/transcript.jsonl"}, nil
	}
	a.input.setText("/new")
	_, opening := a.Update(key("enter"))
	for _, r := range "typed " {
		a.Update(key(string(r)))
	}
	a.Update(tea.PasteStartMsg{})
	a.Update(tea.PasteMsg{Content: "unfinished paste"})
	request := a.conversationRequest
	_, quitting := a.Update(sigQuitMsg{})
	spend(t, a, quitting)
	close(release)
	spend(t, a, opening)
	restarted := newApp(t.Context(), Options{Agent: &fakeAgent{model: "m"}, Workspace: a.workspace, SessionFile: a.file, DraftFile: a.draftFile})
	if got := restarted.pastesUnfolded(restarted.input.String()); got != "typed unfinished paste" {
		t.Fatalf("next start restored %q", got)
	}
	if a.conversationOpening || a.conversationRequest == request {
		t.Fatal("quit left the pending open valid")
	}
}

// Contract E4 explains an ownership reply that cannot spend its original draft.
func TestChangedOwnershipReplyExplainsTheUnsentFirstEnter(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/chat/transcript.jsonl"
	source := &openingMachineSource{ready: make(chan struct{}), row: chatlist.Row{Cell: "chat", Status: chatlist.Here, Fence: 1}}
	a.machines = source
	a.input.setText("original words")
	_, pending := a.Update(key("enter"))
	a.Update(key("x"))
	close(source.ready)
	spend(t, a, pending)
	if len(a.agent.(*fakeAgent).sent) != 0 || !strings.Contains(lastNote(t, a), "draft changed before sending; press enter to send your current words") {
		t.Fatalf("first Enter had no explanation: %q", lastNote(t, a))
	}
}

type lateFailedCloseAgent struct{ *blockedCloseAgent }

func (a *lateFailedCloseAgent) Close() error {
	_ = a.blockedCloseAgent.Close()
	return errors.New("saving failed")
}

// Contract E5 names a closed chat when its background save eventually fails.
func TestSlowCloseSaysItsEventualFailureInTheActiveChat(t *testing.T) {
	agent := &lateFailedCloseAgent{&blockedCloseAgent{fakeAgent: &fakeAgent{model: "m"}, release: make(chan struct{})}}
	a := newTestApp(agent)
	t.Cleanup(a.leaveEverything)
	a.file, a.title = "/tmp/closing/transcript.jsonl", "pricing research"
	keepThree(t, a)
	finished := make(chan struct{})
	a.closingStarted = func(done <-chan struct{}) { go func() { <-done; close(finished) }() }
	cmd, more := a.closeFront()
	if !more {
		t.Fatal("fixture has no next chat")
	}
	spend(t, a, cmd)
	close(agent.release)
	<-finished
	a.Update(noticesMsg{})
	var shown string
	for _, entry := range a.entries {
		shown += entry.text + "\n"
	}
	if !strings.Contains(shown, "pricing research") || !strings.Contains(shown, closeSaveFailedWord) {
		t.Fatalf("active chat missed the late failure: %s", shown)
	}
	before := len(a.entries)
	a.Update(noticesMsg{})
	if len(a.entries) != before {
		t.Fatal("late failure repeated")
	}
}
