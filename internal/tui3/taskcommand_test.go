package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type taskCommandFake struct {
	Agent
	judgeCalls, singleCalls, adaptiveCalls int
	yes                                    bool
	parts                                  []string
	why                                    string
	brief, hint                            string
	err                                    error
}

func (f *taskCommandFake) StartTask(_ context.Context, brief string) (uint64, string, error) {
	f.singleCalls++
	f.brief = brief
	return 7, "named work", f.err
}
func (f *taskCommandFake) StartPlannerRun(_ context.Context, brief, hint string) (string, string, error) {
	f.adaptiveCalls++
	f.brief, f.hint = brief, hint
	return "3", "named work", f.err
}
func (f *taskCommandFake) JudgeDecomposable(context.Context, string) (bool, []string, string) {
	f.judgeCalls++
	return f.yes, f.parts, f.why
}
func (*taskCommandFake) TaskPlannerModel() string { return "master/model:high" }

func TestTaskExplicitFormsSkipSizing(t *testing.T) {
	base := &fakeAgent{model: "m"}
	f := &taskCommandFake{Agent: base}
	a := newTestApp(f)
	for _, tc := range []struct {
		line     string
		adaptive bool
	}{{"/task solo fix it", false}, {"/task adaptive map it", true}} {
		cmd := a.slash(tc.line)
		msg := cmd()
		_, _ = a.Update(msg)
		if f.judgeCalls != 0 {
			t.Fatal("an explicit form called the judge")
		}
	}
	if f.singleCalls != 1 || f.adaptiveCalls != 1 {
		t.Fatalf("single=%d adaptive=%d", f.singleCalls, f.adaptiveCalls)
	}
}

func TestTaskYesChoosesAdaptiveAndEscapeChoosesSingle(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api scan", "ui scan"}, why: "independent"}
	a := newTestApp(f)
	msg := a.slash("/task inspect both")()
	_, _ = a.Update(msg)
	if !a.taskPick.open || a.taskPick.cursor != 0 {
		t.Fatal("yes did not open on adaptive")
	}
	cmd := a.taskChooserKey(key("enter"))
	_, _ = a.Update(cmd())
	if f.adaptiveCalls != 1 || f.brief != "inspect both" || !strings.Contains(f.hint, "api scan") {
		t.Fatalf("adaptive got brief=%q hint=%q", f.brief, f.hint)
	}

	f.yes = true
	msg = a.slash("/task inspect again")()
	_, _ = a.Update(msg)
	cmd = a.taskChooserKey(key("esc"))
	_, _ = a.Update(cmd())
	if f.singleCalls != 1 {
		t.Fatal("escape did not start single")
	}
}

func TestTaskNoStartsSingleAndErrorsBecomeNotes(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	msg := a.slash("/task linear work")()
	_, cmd := a.Update(msg)
	if cmd == nil {
		t.Fatal("a no did not return the single start command")
	}
	_, _ = a.Update(cmd())
	if a.taskPick.open {
		t.Fatal("a no opened the chooser")
	}
	if f.singleCalls != 1 {
		t.Fatal("a no did not start single")
	}

	f.err = errors.New("unknown brief")
	started := taskStartedMsg{kind: "single", err: f.err}
	_, _ = a.Update(started)
	if got := lastNote(t, a); !strings.Contains(got, "unknown brief") {
		t.Fatalf("error note = %q", got)
	}
}
