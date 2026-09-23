package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// delegateFake is the scripted session with the delegate door on it: a list of
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

func TestAnInstalledDelegateIsACommandRowThatOpensTheDoor(t *testing.T) {
	a, fake := newDelegateApp(t, session.DelegateRow{Name: "fake", Description: "a fake delegate", Lands: "tree", Bin: "/usr/local/bin/fake"})
	if !isDelegateCommand("fake") {
		t.Fatal("the delegate's row was not installed")
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
	// And the long form is the same door.
	if cmd := a.slash("/delegate fake do the other thing"); cmd == nil {
		t.Fatal("/delegate <name> <brief> opened no door")
	} else {
		settleDoor(t, a, cmd)
	}
	if len(fake.started) != 2 || fake.started[1] != "fake: do the other thing" {
		t.Fatalf("StartDelegate was asked %v", fake.started)
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

func TestSlashDelegateListsTheRowsAndTheOnesNotHere(t *testing.T) {
	a, fake := newDelegateApp(t, session.DelegateRow{Name: "fake", Description: "a fake delegate", Lands: "text", Bin: "/opt/fake"})
	fake.report.Absent = []string{"senior-dev: senior-dev is not on this machine"}
	fake.report.Refused = []string{"broken: its manual page does not say /broken — not added"}
	a.width = 200
	settleDoor(t, a, a.slash("/delegate"))
	got := plain(frame(a))
	for _, want := range []string{"/fake <brief>", "a fake delegate", "answers in the conversation", "not here: senior-dev", "not added: broken"} {
		if !strings.Contains(got, want) {
			t.Fatalf("/delegate did not say %q:\n%s", want, got)
		}
	}
}

func TestSlashDelegateWithNothingInstalledSaysSo(t *testing.T) {
	a, _ := newDelegateApp(t)
	a.width = 200
	settleDoor(t, a, a.slash("/delegates"))
	if got := plain(frame(a)); !strings.Contains(got, "no delegates here") {
		t.Fatalf("no sentence for a machine with none:\n%s", got)
	}
	if isDelegateCommand("fake") {
		t.Fatal("a row exists for a delegate nobody installed")
	}
	// A session with no delegate door at all says the same sentence.
	bare := newTestApp(&fakeAgent{})
	bare.width = 200
	bare.slash("/delegate")
	if got := plain(frame(bare)); !strings.Contains(got, "no delegates here") {
		t.Fatalf("no sentence for a session without the door:\n%s", got)
	}
}

func TestADelegateNamedLikeABuiltInCommandIsNotInstalled(t *testing.T) {
	a, _ := newDelegateApp(t, session.DelegateRow{Name: "task", Description: "an impostor"})
	if isDelegateCommand("task") {
		t.Fatal("a delegate shadowed /task")
	}
	a.width = 200
	settleDoor(t, a, a.slash("/delegate"))
	if got := plain(frame(a)); !strings.Contains(got, "not added: task: its name is already a command here") {
		t.Fatalf("the collision was not said:\n%s", got)
	}
	for _, c := range commands {
		if c.name == "task" && c.desc == "an impostor" {
			t.Fatal("the impostor row is on the table")
		}
	}
}

// A HOSTED SURFACE LISTS AND RUNS THE FAR MACHINE'S DELEGATES: the seam crosses
// the wire (internal/remote's Delegate.List and Delegate.Start), the rows are
// generated from what the engine machine has, and the door starts the run
// there. Nothing is refused for being hosted.
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
