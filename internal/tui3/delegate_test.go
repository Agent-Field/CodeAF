package tui3

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// delegateFake is the scripted session with the program door on it: a list of
// rows, and a StartDelegate that records what it was asked.
type delegateFake struct {
	*fakeAgent
	report  session.DelegateReport
	started []string
	fail    error
}

func (d *delegateFake) Delegates() session.DelegateReport { return d.report }

func (d *delegateFake) StartDelegate(_ context.Context, name, brief string) (uint64, string, string, error) {
	d.started = append(d.started, name+": "+brief)
	if d.fail != nil {
		return 0, "", "", d.fail
	}
	return 7, "the title", "", nil
}

func newDelegateApp(t *testing.T, rows ...session.DelegateRow) (*app, *delegateFake) {
	t.Helper()
	fake := &delegateFake{fakeAgent: &fakeAgent{}, report: session.DelegateReport{Rows: rows}}
	a := newTestApp(fake)
	t.Cleanup(func() { installDelegateCommands(nil) })
	settleDoor(t, a, a.installDelegates())
	return a, fake
}

// settleDoor runs one off-loop door to its answer and folds it in, the way the
// update loop would on the doorMsg: the command is run, the fold applied as
// though the window were still on the same conversation, and any command the
// fold hands back is run too, its message returned.
func settleDoor(t *testing.T, a *app, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg, ok := cmd().(doorMsg)
	if !ok {
		t.Fatalf("the door did not answer on the door line: %T", cmd())
	}
	if next := msg.fold(true); next != nil {
		return next()
	}
	return nil
}

func TestACarriedProgramIsACommandRowThatOpensTheDoor(t *testing.T) {
	a, fake := newDelegateApp(t, session.DelegateRow{Name: "fake", Description: "a fake delegate", Lands: "tree"})
	if !isDelegateCommand("fake") {
		t.Fatal("the program's row was not installed")
	}
	named := false
	for _, c := range commands {
		if c.name == "fake" && c.args == "<brief>" && c.desc == "a fake delegate" {
			named = true
		}
	}
	if !named {
		t.Fatal("the live command table has no /fake <brief> row")
	}
	cmd := a.slash("/fake rewrite the auth middleware")
	if cmd == nil {
		t.Fatal("/fake <brief> opened no door")
	}
	if msg, ok := settleDoor(t, a, cmd).(taskStartedMsg); !ok || msg.id != "7" || msg.title != "the title" || msg.brief != "rewrite the auth middleware" {
		t.Fatalf("the door answered %+v", msg)
	}
	if len(fake.started) != 1 || fake.started[0] != "fake: rewrite the auth middleware" {
		t.Fatalf("StartDelegate was asked %v", fake.started)
	}
}

// THERE IS NO /delegate. "delegate" is a working title, and every program is
// reached by its own name; the word is not a command.
func TestThereIsNoSlashDelegate(t *testing.T) {
	a, fake := newDelegateApp(t, session.DelegateRow{Name: "fake", Description: "a fake delegate"})
	a.width = 200
	if cmd := a.slash("/delegate fake do it"); cmd != nil {
		settleDoor(t, a, cmd)
	}
	if len(fake.started) != 0 {
		t.Fatalf("/delegate started work: %v", fake.started)
	}
	for _, c := range commands {
		if c.name == "delegate" {
			t.Fatal("the command table still has a /delegate row")
		}
	}
}

func TestADelegateRowWithNoBriefSaysItsUsage(t *testing.T) {
	a, fake := newDelegateApp(t, session.DelegateRow{Name: "fake", Description: "a fake delegate"})
	if cmd := a.slash("/fake"); cmd != nil {
		t.Fatal("a bare delegate command started something")
	}
	if len(fake.started) != 0 {
		t.Fatalf("StartDelegate was asked %v", fake.started)
	}
	if got := plain(frame(a)); !strings.Contains(got, "usage: /fake <brief>") {
		t.Fatalf("no usage line:\n%s", got)
	}
}

func TestAProgramNamedLikeABuiltInCommandIsNotInstalled(t *testing.T) {
	newDelegateApp(t, session.DelegateRow{Name: "task", Description: "an impostor"})
	if isDelegateCommand("task") {
		t.Fatal("a program shadowed /task")
	}
	if refused := installDelegateCommands([]session.DelegateRow{{Name: "task", Description: "an impostor"}}); len(refused) != 1 || !strings.Contains(refused[0], "its name is already a command here") {
		t.Fatalf("refused = %v, want the collision answered back", refused)
	}
	for _, c := range commands {
		if c.name == "task" && c.desc == "an impostor" {
			t.Fatal("the impostor row is on the table")
		}
	}
}

// A HOSTED SURFACE LISTS AND RUNS THE FAR MACHINE'S PROGRAMS: the seam crosses
// the wire (internal/remote's Delegate.List and Delegate.Start), the rows are
// generated from what the engine machine's build carries, and the door starts
// the run there. Nothing is refused for being hosted.
func TestAHostedSurfaceInstallsTheFarMachinesDelegateRows(t *testing.T) {
	fake := &delegateFake{fakeAgent: &fakeAgent{}, report: session.DelegateReport{Rows: []session.DelegateRow{{Name: "fake", Description: "a fake delegate"}}}}
	a := newTestApp(fake)
	t.Cleanup(func() { installDelegateCommands(nil) })
	a.host = "spark"
	settleDoor(t, a, a.installDelegates())
	if !isDelegateCommand("fake") {
		t.Fatal("a hosted surface did not install the far machine's delegate row")
	}
	if cmd := a.slash("/fake do it there"); cmd == nil {
		t.Fatal("/fake opened no door on a hosted surface")
	} else {
		settleDoor(t, a, cmd)
	}
	if len(fake.started) != 1 || fake.started[0] != "fake: do it there" {
		t.Fatalf("StartDelegate was asked %v", fake.started)
	}
}

// A PROGRAM WORKS IN THE PERSON'S FOLDER. Its dirty-checkout refusal must not
// be preceded by /task's promise that unsaved edits travel into a copy; /task
// still makes that copy and keeps its existing note.
func TestDirtyProgramCommandRefusesWithoutPromisingACopyAndTaskKeepsItsNote(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.test")
	file := filepath.Join(repo, "README.md")
	if err := os.WriteFile(file, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-qm", "first")
	if err := os.WriteFile(file, []byte("unfinished\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	program, fake := newDelegateApp(t, session.DelegateRow{Name: "senior-dev", Description: "a program"})
	program.workspace = repo
	fake.fail = errors.New(repo + " has changes that are not committed (README.md); commit or stash them, then ask again")
	cmd := program.slash("/senior-dev finish the feature")
	if cmd == nil {
		t.Fatal("/senior-dev did not open its command door")
	}
	if msg := settleDoor(t, program, cmd); msg != nil {
		_, _ = program.Update(msg)
	}
	programNotes := strings.Join(noteTexts(program), "\n")
	if !strings.Contains(programNotes, "has changes that are not committed") {
		t.Fatalf("the refusal did not reach the person: %q", programNotes)
	}
	if strings.Contains(programNotes, "copy") || strings.Contains(programNotes, "unsaved edits go with it") {
		t.Fatalf("the program promised a copy before refusing: %q", programNotes)
	}

	ordinary := newTestApp(&taskCommandFake{Agent: &fakeAgent{model: "m"}})
	ordinary.workspace = repo
	_, _ = ordinary.Update(taskMsg(ordinary.slash("/task finish the feature")))
	wantNotes := []string{session.UnsavedEditsNote(repo), "single task 7 started · named work"}
	if got := noteTexts(ordinary); len(got) != len(wantNotes) || got[0] != wantNotes[0] || got[1] != wantNotes[1] {
		t.Fatalf("/task's notes = %q, want %q", got, wantNotes)
	}
}
