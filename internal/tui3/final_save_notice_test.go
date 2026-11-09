package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

type finalSavingWindowAgent struct {
	*fakeAgent
	lines []string
	final bool
}

func (a *finalSavingWindowAgent) FinishSaving() error {
	if a.final {
		a.lines = append(a.lines, chatlist.Superseded("desk"))
		a.final = false
	}
	return nil
}
func (a *finalSavingWindowAgent) TakeSavingNotices() []string {
	lines := a.lines
	a.lines = nil
	return lines
}

func TestClosingOneChatShowsItsFinalSavingSentenceOnce(t *testing.T) {
	for _, displaced := range []bool{true, false} {
		t.Run(map[bool]string{true: "displaced", false: "clean"}[displaced], func(t *testing.T) {
			agent := &finalSavingWindowAgent{fakeAgent: &fakeAgent{model: "m"}, final: displaced}
			a := newTestApp(agent)
			t.Cleanup(a.leaveEverything)
			a.file = "/tmp/closing/transcript.jsonl"
			emptyMachine(a)
			a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/remaining/transcript.jsonl"}, &aside{})
			before := len(noteTexts(a))
			cmd, more := a.closeFrontFor(session.StopByLeaving)
			if !more {
				t.Fatal("no remaining chat")
			}
			spend(t, a, cmd)
			said := strings.Join(noteTexts(a)[before:], "\n")
			count := strings.Count(said, chatlist.Superseded("desk"))
			if displaced && count != 1 || !displaced && count != 0 {
				t.Fatalf("closing window notes: %q", said)
			}
			if len(agent.TakeSavingNotices()) != 0 {
				t.Fatal("closing window left a duplicate receipt")
			}
		})
	}
}
