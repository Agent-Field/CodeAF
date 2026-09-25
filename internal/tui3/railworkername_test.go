package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE RAIL DRAWS WHAT THE ENGINE CALLS THE WORK, AND NOTHING ELSE.
//
// The screen this pins is a real one: an adaptive run divided into nine workers
// drew nine rows under it, and every one of them read "You are a" — three words
// off the front of the brief that worker opens on. The fix is entirely in the
// engine (internal/orchestrate's Node.Title), so what this surface owes is the
// other half of the claim: given a name, the row is the name; and given the
// sentence again, the row is still the sentence, because this column has never
// been the place that decides.

func TestAWorkerRowIsTheNameTheEngineGaveItAndNotItsBrief(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "competitive intelligence on the three vendors", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "pricing sheet", session.TaskRunning, session.TaskNotice{Parent: 1}))
	a.taskUpdate(update(3, "feature matrix", session.TaskRunning, session.TaskNotice{Parent: 1}))
	railKinship(a, 1, 2, 3)

	rows := railText(a, a.viewHeight())
	drawn := strings.Join(rows, "\n")
	for _, name := range []string{"pricing sheet", "feature matrix"} {
		if !strings.Contains(drawn, name) {
			t.Fatalf("the rail never draws %q:\n%s", name, drawn)
		}
	}
	if strings.Contains(drawn, "You are") {
		t.Fatalf("a worker row was drawn out of a brief:\n%s", drawn)
	}
	// AND THE ROW IS THE WHOLE NAME rather than a cut of it: a two-word name is
	// inside the column's own cap, so nothing is thrown away on the way here.
	if node := a.tasks[2]; node == nil || node.title != "pricing sheet" {
		t.Fatalf("the worker's row is titled %q", node.title)
	}
}

// AND THE RUN'S OWN ROW SAYS ITS WORK IS BEING FORMED while there is nothing
// under it to look at — the engine's `forming the work`, arriving as an ordinary
// phase on a running row (session's TaskNotice.Doing) and drawn by [app.railDoing]
// with no word of this surface's own in front of it.
func TestARunSaysItIsFormingWhileItHasNothingToShowYet(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "competitive intelligence on the three vendors",
		session.TaskRunning, session.TaskNotice{Doing: "forming the work"}))

	if drawn := strings.Join(railText(a, a.viewHeight()), "\n") + "\n" + railHint(a, 1); !strings.Contains(drawn, "forming the work") {
		t.Fatalf("the run's row says nothing while its workers are being formed:\n%s", drawn)
	}

	// AND IT STOPS SAYING IT the moment the engine takes the line off. The rows
	// themselves are the picture from there, and a surface still saying "forming"
	// over three drawn workers would be reporting a present that has passed.
	a.taskUpdate(update(1, "competitive intelligence on the three vendors", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "pricing sheet", session.TaskRunning, session.TaskNotice{Parent: 1}))
	railKinship(a, 1, 2)

	drawn := strings.Join(railText(a, a.viewHeight()), "\n") + "\n" + railHint(a, 1)
	if strings.Contains(drawn, "forming the work") {
		t.Fatalf("the run still says it is forming with a worker on the board:\n%s", drawn)
	}
	if !strings.Contains(drawn, "pricing sheet") {
		t.Fatalf("the worker never reached the rail:\n%s", drawn)
	}
}

// AND A WORKER WHOSE NAME HAS NOT LANDED YET IS DRAWN AS THIS SURFACE'S OWN
// WORD FOR ONE, never as the id the engine filed it under.
//
// The screen behind this is the second one: a planner numbered its own nodes
// `r1` … `r7`, so a run's whole family stood on the rail under the machine's
// filing. The engine now sends a nameless row rather than the id and names it a
// moment later from its small namer (internal/session's taskname.go), which is
// the same two-publish shape an admitted task has always arrived under — the row
// first, the name when it lands.
func TestAWorkerWaitingForItsNameIsDrawnAsATaskAndNotAnId(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "competitive intelligence on the three vendors", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "", session.TaskRunning, session.TaskNotice{Parent: 1, Node: "r1"}))
	railKinship(a, 1, 2)

	drawn := strings.Join(railText(a, a.viewHeight()), "\n")
	if strings.Contains(drawn, "r1") {
		t.Fatalf("the engine's own filing reached the rail:\n%s", drawn)
	}
	if !strings.Contains(drawn, taskIDWord(2)) {
		t.Fatalf("a worker with no name yet is drawn as something else:\n%s", drawn)
	}

	// AND THE NAME LANDING IS THE ROW. Nothing about the work moved — the same
	// state arrives again with three more words on it — and the column reads the
	// name from there on.
	a.taskUpdate(update(2, "vendor pricing", session.TaskRunning, session.TaskNotice{Parent: 1, Node: "r1"}))
	drawn = strings.Join(railText(a, a.viewHeight()), "\n")
	if !strings.Contains(drawn, "vendor pricing") {
		t.Fatalf("the name never reached the rail:\n%s", drawn)
	}
	if strings.Contains(drawn, taskIDWord(2)) {
		t.Fatalf("the row is still drawn as %q with its name in hand:\n%s", taskIDWord(2), drawn)
	}
}
