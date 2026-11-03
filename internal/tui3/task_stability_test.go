package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

type countedSurfaceComputers struct {
	surfaceTaskComputers
	lists int
}

func (c *countedSurfaceComputers) List(context.Context) ([]session.TaskComputer, error) {
	c.lists++
	return c.surfaceTaskComputers, nil
}

// S1 forbids a task-computer directory call for an ordinary proposal.
func TestAPairedHomeWithDefaultsNeverListsTaskComputers(t *testing.T) {
	a, _, _ := taskApp(t)
	c := &countedSurfaceComputers{surfaceTaskComputers: surfaceTaskComputers{{Device: surfaceComputerB, Name: "work computer", CanTakeTasks: true}}}
	a.taskComputers = c
	_ = runCmd(a.Init())
	if c.lists != 0 {
		t.Fatalf("opening a paired chat listed task computers %d times", c.lists)
	}
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: proposal(a, 7, 0)})
	if c.lists != 0 {
		t.Fatalf("an ordinary proposal listed task computers %d times", c.lists)
	}
}

// S3 keeps both the original card and its key bindings until explicit placement.
func TestTheProposalCardSaysNothingAboutComputersByDefault(t *testing.T) {
	a, _, _ := taskApp(t)
	a.proposeTask(proposal(a, 7, 0))
	original := strings.Join(a.taskCardRows(a.task, 120, false), "\n")
	a.taskComputers = surfaceTaskComputers{{Device: surfaceComputerB, Name: "work computer", CanTakeTasks: true}}
	drive(t, a, taskMsg(a.readTaskComputers()))
	if got := strings.Join(a.taskCardRows(a.task, 120, false), "\n"); got != original {
		t.Fatalf("pairing changed the default proposal:\n%s", plain(got))
	}
	if cmd, took := a.taskKey(key("ctrl+d")); cmd != nil || took {
		t.Fatal("the default proposal claimed a computer key")
	}
}

// S10 makes the setting the default opt-in door, with no roster fetch on entry.
func TestTheSettingsRowIsTheOnlyNewThingAPairedPersonSeesByDefault(t *testing.T) {
	a, _ := sheetApp(t)
	c := &countedSurfaceComputers{surfaceTaskComputers: surfaceTaskComputers{{Device: surfaceComputerB, Name: "work computer", CanTakeTasks: true}}}
	a.taskComputers = c
	drive(t, a, key("ctrl+,"))
	cursorTo(t, a, config.KeyTaskRunsOn)
	if c.lists != 0 {
		t.Fatalf("opening settings listed task computers %d times", c.lists)
	}
	if text := plain(frame(a)); !strings.Contains(text, "Run tasks on") || !strings.Contains(text, "this computer") {
		t.Fatalf("missing default opt-in row:\n%s", text)
	}
	drive(t, a, key("enter"))
	if c.lists != 1 || !strings.Contains(plain(frame(a)), "work computer") {
		t.Fatalf("opening the row did not fetch its computers: calls=%d\n%s", c.lists, plain(frame(a)))
	}
}
