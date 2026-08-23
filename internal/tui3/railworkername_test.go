package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
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

	if drawn := strings.Join(railText(a, a.viewHeight()), "\n"); !strings.Contains(drawn, "forming the work") {
		t.Fatalf("the run's row says nothing while its workers are being formed:\n%s", drawn)
	}

	// AND IT STOPS SAYING IT the moment the engine takes the line off. The rows
	// themselves are the picture from there, and a surface still saying "forming"
	// over three drawn workers would be reporting a present that has passed.
	a.taskUpdate(update(1, "competitive intelligence on the three vendors", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(2, "pricing sheet", session.TaskRunning, session.TaskNotice{Parent: 1}))
	railKinship(a, 1, 2)

	drawn := strings.Join(railText(a, a.viewHeight()), "\n")
	if strings.Contains(drawn, "forming the work") {
		t.Fatalf("the run still says it is forming with a worker on the board:\n%s", drawn)
	}
	if !strings.Contains(drawn, "pricing sheet") {
		t.Fatalf("the worker never reached the rail:\n%s", drawn)
	}
}
