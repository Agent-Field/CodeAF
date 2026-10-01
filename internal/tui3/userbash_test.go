package tui3

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func (f *fakeAgent) SubmitBash(ctx context.Context, text string) (<-chan session.Event, error) {
	f.bashSent = append(f.bashSent, text)
	return f.Submit(ctx, text)
}

func TestUserBashToolLimitUsesItsRunnerTimeout(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.bashBackgroundAfter = 30
	for _, test := range []struct {
		name, callID string
		want         time.Duration
	}{
		{name: "person", callID: "user_bash_probe", want: 600 * time.Second},
		{name: "model", callID: "model_probe", want: 30 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := &entry{kind: entryTool, tool: "bash", status: toolRunning,
				callID: test.callID, detail: toolDetail{Args: `{"command":"sleep 35"}`}}
			if got := a.toolLimit(row); got != test.want {
				t.Fatalf("toolLimit = %s, want %s", got, test.want)
			}
		})
	}
}

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
	if len(fake.bashSent) != 1 || len(fake.sent) != 1 || fake.sent[0] != line || len(fake.marked) != 0 {
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
	// The opening is answered off the update loop (#1662), so its result is
	// delivered back through the program the way the event loop delivers it.
	spend(t, a, a.homeEnter())
	if opened != theirs || len(next.bashSent) != 1 || len(next.sent) != 1 || next.sent[0] != line {
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

func TestUserBashComposerPromptReactsAndRestores(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	for _, text := range []string{"!", "!pwd", "", "hello!"} {
		a.input.setText(text)
		rows, _, _ := draftBlockWithTags(&a.input, a.pal, 80, 3, "", "", nil, a.pal.ink)
		want := a.pal.dim(prompt)
		if strings.HasPrefix(text, "!") {
			want = a.pal.warn(a.pal.glyph(tokens.GPromptShell) + " ")
		}
		if !strings.Contains(rows[0], want) {
			t.Fatalf("draft %q has wrong prompt: %q", text, rows[0])
		}
	}
	a.input.setText("!")
	rows, _, _ := draftBlock(&a.input, a.pal, 80, 3, "", "")
	if strings.Contains(ansi.Strip(rows[0]), "$ ") {
		t.Fatal("filter acquired shell prompt")
	}
}

func TestUserBashStreamsLiteralRowsWithoutFoldingOrClipping(t *testing.T) {
	f := &feed{live: -1, think: -1, turn: 1}
	id := "user_bash_live"
	f.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: id})
	output := "  **literal**\n\n" + strings.Repeat("x", 100) + "END\n"
	f.ingest(session.Event{Kind: session.EventToolOutput, Tool: "bash", CallID: id, Text: output})
	e := &f.entries[0]
	if !e.open || !liveWorkKeepsRow(e) || e.detail.Output != output {
		t.Fatal("live shell output hidden or modified")
	}
	a := newTestApp(&fakeAgent{})
	rows, more, _ := a.toolBlockRows(e, 40, false)
	plain := ansi.Strip(strings.Join(rows, "\n"))
	if more != 0 || !strings.HasPrefix(plain, "  **literal**\n\n") || !strings.Contains(plain, "END") || !strings.HasSuffix(plain, "\n") {
		t.Fatalf("literal output clipped or formatted: %q", plain)
	}
	phone, _, _ := a.toolBlockRows(e, 30, true)
	if !strings.Contains(ansi.Strip(strings.Join(phone, "\n")), "END") {
		t.Fatal("phone hid live shell output")
	}
}
