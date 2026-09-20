package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A DOOR AT REST WHOSE TASK SUBTREE IS STILL TURNING IS NOT IDLE.
//
// Handing a task out ends the turn — `a.state` goes back to [stateIdle] — and the
// node it started works on for minutes with nothing happening in the
// conversation (tabsignal.go says why a task outlives the turn that proposed
// it). The tab strip already draws that door as `working` ([app.frontSignal]),
// and the status row drew `idle` under a tab wearing `◐` — one conversation
// described two ways on one screen.
//
// The word is the tab's own ([tabWorkingWord]), and the reading is the surface's
// frame-safe one: [app.tasksInFlight] walks a map this surface keeps and
// [app.jobsRunning] walks the job list, so nothing here opens
// [session.Agent.TaskIndex], which reads a file (tabsignal.go's header states the
// law). The figures beside the word are untouched.
func TestADoorAtRestWhoseTaskSubtreeTurnsSaysWorking(t *testing.T) {
	a, _, _ := hudApp(t)

	// WITH NOTHING HANDED OUT the door is at rest and says so.
	if got, _ := a.stateWord(); got != stateIdle.String() {
		t.Fatalf("a door with no work at all says %q, want %q", got, stateIdle.String())
	}

	// A TASK THIS DOOR STARTED IS STILL TURNING. The turn that proposed it is
	// over — `a.state` is idle — and the node works on.
	drive(t, a, streamEventMsg{gen: a.gen,
		ev: update(7, "port the parser", session.TaskRunning, session.TaskNotice{})})
	if a.state != stateIdle {
		t.Fatalf("the fixture left the door's own turn running: %v", a.state)
	}

	got, painted := a.stateWord()
	if got != tabWorkingWord {
		t.Fatalf("a door at rest with a task still turning says %q, want %q", got, tabWorkingWord)
	}
	if painted != a.pal.accent(tabWorkingWord) {
		t.Fatalf("the working word is not painted accent: %q", painted)
	}
	// AND THE SEAM ITSELF CARRIES IT, not only the word function: the
	// status reading lives above the message box beside the model.
	if row := plain(a.legend(a.width)); !strings.Contains(row, tabWorkingWord) {
		t.Fatalf("the seam does not say %q:\n%q", tabWorkingWord, row)
	}
	// AND IT IS A READING, NOT A TURN: no spinner and no clock, which belong to a
	// turn that is not running (render.go's [app.stateSegment]).
	if seg, _ := a.stateSegment(); strings.ContainsAny(seg, spinnerFrames()) {
		t.Fatalf("a door at rest wore a turn's spinner: %q", seg)
	}

	// AND A SETTLED ROSTER PUTS THE DOOR BACK AT REST. One node ending among
	// others does not, which is why the reading is not a count.
	drive(t, a, streamEventMsg{gen: a.gen,
		ev: update(7, "port the parser", session.TaskDone, session.TaskNotice{})})
	if got, _ := a.stateWord(); got != stateIdle.String() {
		t.Fatalf("a door whose task landed says %q, want %q", got, stateIdle.String())
	}
}
