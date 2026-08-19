package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
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

// taskStartApp is a surface whose `starting a task` row is already answered. The
// row is read at the moment the command is typed ([config.TaskStartAt]), so a
// profile on disk is the only way to state it and the only way a test can.
func taskStartApp(t *testing.T, f *taskCommandFake, mode string) *app {
	t.Helper()
	dir := t.TempDir()
	body := []byte(`{"` + config.KeyTaskStart + `":"` + mode + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o600); err != nil {
		t.Fatalf("writing the profile: %v", err)
	}
	a := newTestApp(f)
	a.profileDir = dir
	return a
}

// SHIPPED, /task STILL ASKS. The default is the behaviour every other test on
// this page is written against, and a profile with nothing in it is a person who
// has never opened the settings panel.
func TestTaskStartDefaultsToAsking(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api", "ui"}}
	a := taskStartApp(t, f, config.TaskStartAsk)
	if got := config.TaskStartAt(a.profileDir); got != config.TaskStartAsk {
		t.Fatalf("the row reads %q", got)
	}
	if got := config.TaskStartAt(t.TempDir()); got != config.TaskStartAsk {
		t.Fatalf("an unanswered profile reads %q", got)
	}
	_, _ = a.Update(a.slash("/task inspect both")())
	if !a.taskPick.open {
		t.Fatal("the default did not raise the chooser")
	}
	if f.adaptiveCalls != 0 || f.singleCalls != 0 {
		t.Fatal("the default started work before it was answered")
	}
}

// SET TO ADAPTIVE, NOBODY IS ASKED — and what starts still follows what the
// sizing call found: parts to split means a planner, nothing to split means one
// worker, because a planner over work with no independent parts in it is a whole
// extra model deciding nothing.
func TestTaskStartAdaptiveSkipsTheChooser(t *testing.T) {
	for _, tc := range []struct {
		name              string
		parallel          bool
		adaptive, singles int
	}{
		{"parts to split", true, 1, 0},
		{"nothing to split", false, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: tc.parallel, parts: []string{"api", "ui"}, why: "independent"}
			a := taskStartApp(t, f, config.TaskStartAdaptive)
			_, cmd := a.Update(a.slash("/task inspect both")())
			if a.taskPick.open {
				t.Fatal("adaptive still asked")
			}
			if cmd == nil {
				t.Fatal("nothing was started")
			}
			_, _ = a.Update(cmd())
			if f.judgeCalls != 1 {
				t.Fatalf("%d sizing calls, want one", f.judgeCalls)
			}
			if f.adaptiveCalls != tc.adaptive || f.singleCalls != tc.singles {
				t.Fatalf("adaptive=%d single=%d, want %d/%d", f.adaptiveCalls, f.singleCalls, tc.adaptive, tc.singles)
			}
			if tc.adaptive == 1 && !strings.Contains(f.hint, "api") {
				t.Fatalf("the planner was given no sketch: %q", f.hint)
			}
		})
	}
}

// SET TO SINGLE, THE SIZING CALL IS NOT MADE AT ALL. Its only product is the
// chooser and the adaptive sketch, and paying a model for an answer that is
// going to be ignored is a bill with nothing behind it.
func TestTaskStartSingleSkipsTheSizingCall(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api", "ui"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	cmd := a.slash("/task inspect both")
	if cmd == nil {
		t.Fatal("nothing was started")
	}
	_, _ = a.Update(cmd())
	if f.judgeCalls != 0 {
		t.Fatal("single paid for a sizing call it had already answered")
	}
	if a.taskPick.open || f.adaptiveCalls != 0 || f.singleCalls != 1 {
		t.Fatalf("chooser=%v adaptive=%d single=%d", a.taskPick.open, f.adaptiveCalls, f.singleCalls)
	}
}

// AN EXPLICIT WORD IS THE LAST WORD, in both directions and against both silent
// rows: `/task solo` under `adaptive` is one worker, `/task adaptive` under
// `single` is a planner.
func TestExplicitFormsOverrideTheRow(t *testing.T) {
	for _, tc := range []struct {
		row, line         string
		adaptive, singles int
	}{
		{config.TaskStartAdaptive, "/task solo fix it", 0, 1},
		{config.TaskStartSingle, "/task adaptive map it", 1, 0},
	} {
		f := &taskCommandFake{Agent: &fakeAgent{model: "m"}, yes: true, parts: []string{"api"}}
		a := taskStartApp(t, f, tc.row)
		_, _ = a.Update(a.slash(tc.line)())
		if f.judgeCalls != 0 {
			t.Fatalf("%q under %q called the sizing judge", tc.line, tc.row)
		}
		if f.adaptiveCalls != tc.adaptive || f.singleCalls != tc.singles {
			t.Fatalf("%q under %q: adaptive=%d single=%d", tc.line, tc.row, f.adaptiveCalls, f.singleCalls)
		}
	}
}

// THE WAIT IS NAMED AND THEN IT LEAVES. Shaping the brief is a model call of its
// own, so a command that looked like it had done nothing for several seconds
// would read as a command that never registered — and a note still standing
// after the task started would be the transcript saying something untrue.
func TestTheShapingWaitIsShownAndThenTakenAway(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := taskStartApp(t, f, config.TaskStartSingle)
	cmd := a.slash("/task write the release notes")
	if got := lastNote(t, a); got != taskShapingNote {
		t.Fatalf("the wait note reads %q, want %q", got, taskShapingNote)
	}
	_, _ = a.Update(cmd())
	for _, entry := range a.entries {
		if entry.kind == entryNote && entry.text == taskShapingNote {
			t.Fatal("the shaping note outlived the task it was about")
		}
	}
	if got := lastNote(t, a); !strings.Contains(got, "task 7 started") {
		t.Fatalf("last note = %q", got)
	}
}
