package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestUserBashComposerBypassesSetupAndKeepsShellSyntax(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	a.setup.open = false
	a.welcome.open = false
	a.closeHome()
	line := `!printf '%s\n' /task /standing @literal`
	a.input.setText(line)
	a.syncLists()
	if a.menu.open || a.comp.open || len(a.liveTags()) != 0 {
		t.Fatal("shell syntax opened a picker or became a command tag")
	}
	result := runSubmit(t, a.enterLine(true))
	if result.err != nil || a.setup.open {
		t.Fatalf("shell hit setup: %v", result.err)
	}
	fake := a.agent.(*fakeAgent)
	if len(fake.sent) != 1 || fake.sent[0] != line || len(fake.marked) != 0 {
		t.Fatalf("shell was modified or sent through a model modifier: %+v", fake.sent)
	}
}

func TestUserBashRefusalKeepsTheDraft(t *testing.T) {
	for _, line := range []string{"!", "!pwd"} {
		a := newTestApp(&fakeAgent{})
		a.closeHome()
		a.input.setText(line)
		if line != "!" {
			a.state = stateWorking
		}
		if cmd := a.enter(); cmd != nil {
			t.Fatal("refused command was submitted")
		}
		if a.input.String() != line {
			t.Fatal("refusal lost the command")
		}
	}
}

func TestUserBashHomeRunsInTheSelectedProject(t *testing.T) {
	lab := newHomeLab(t)
	mine, theirs := lab.workspace("alpha"), lab.workspace("beta")
	one := lab.session("-tmp-alpha", "aaaa000000000001", "alpha", mine, time.Now())
	two := lab.session("-tmp-beta", "bbbb000000000001", "beta", theirs, time.Now())
	a := lab.app(one)
	a.workspace = mine
	var opened string
	next := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.start = func(workspace string) (Conversation, error) {
		opened = workspace
		return Conversation{Agent: next, SessionFile: workspace + "/next/transcript.jsonl", Workspace: workspace}, nil
	}
	openHomeOn(a, two)
	runCmd(a.openHome())
	a.home.point(two)
	for i := 0; i <= len(a.composerDestinations()); i++ {
		runCmd(a.key(key("alt+p")))
		if a.targetWhere() == theirs {
			break
		}
	}
	line := "!printf '%s' /task @literal"
	typeHome(a, line)
	runCmd(a.homeEnter())
	if opened != theirs || len(next.sent) != 1 || next.sent[0] != line {
		t.Fatalf("home shell went to %q with %q", opened, next.sent)
	}
}

func TestUserBashOutputStaysVisibleLiveAndAfterReopen(t *testing.T) {
	id := "user_bash_example"
	f := &feed{live: -1, think: -1, turn: 1}
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: id, Args: `{"command":"echo visible"}`})
	f.ingest(session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: id, Output: "visible"})
	if !f.entries[0].open || len(deriveWorkfolds(f.entries, 0)) != 0 {
		t.Fatal("the shell output was folded away")
	}
	a := newTestApp(&fakeAgent{})
	a.replayList([]session.DisplayEntry{
		{Role: "user", Text: "!echo visible"},
		{Role: "tool", Tool: "bash", CallID: id, Answered: true, Args: `{"command":"echo visible"}`, Output: "visible"},
	})
	found := false
	for _, e := range a.entries {
		if e.callID == id {
			found = e.open && strings.Contains(e.detail.Output, "visible")
		}
	}
	if !found || len(deriveWorkfolds(a.entries, 0)) != 0 {
		t.Fatal("reopen hid the shell output")
	}
}
