package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// c266PlanRows is one run mid-flight: the run's root wearing its counts, a
// worker with a step in flight, one held behind a sibling it can name, one
// running with nothing to say, and two finished rows the family has folded.
func c266PlanRows() []session.PlanTaskRow {
	live := livePlanRow()
	live.ID, live.Parent, live.Title = "live", "root", "write the handler"
	return []session.PlanTaskRow{
		{ID: "root", Title: "rewrite the auth flow", Status: "running", Done: 2, Running: 1, Queued: 1, Total: 4},
		live,
		{ID: "gate", Parent: "root", Title: "the gate", Status: "running"},
		{ID: "held", Parent: "root", Title: "write the tests", Status: "pending", Waits: []string{"gate"}},
		{ID: "done-a", Parent: "root", Title: "old fixture", Status: "done"},
		{ID: "done-b", Parent: "root", Title: "old helper", Status: "done"},
	}
}

// c266Rail draws the rail of a conversation holding these plan rows and never
// opening the tasks place — the shape the rail exists for ([app.refreshElsewhere]
// is the clock that moves the reading the projection hangs on).
func c266Rail(t *testing.T, rows []session.PlanTaskRow, width int, wide bool) (*app, []string) {
	t.Helper()
	a, _ := planAppWith(t, rows, nil)
	a.width, a.height, a.railWide = width, 30, wide
	a.refreshElsewhere()
	return a, a.railRows(a.viewHeight())
}

// c266RowWith finds the rail row a word rides on.
func c266RowWith(t *testing.T, rows []string, word string) (int, string) {
	t.Helper()
	for i, row := range rows {
		if strings.Contains(plain(row), word) {
			return i, plain(row)
		}
	}
	t.Fatalf("no rail row carries %q:\n%s", word, plain(strings.Join(rows, "\n")))
	return 0, ""
}

// A RUN OF ONE TASK SHOWS NO DOTS: the store's row carries no series, and a
// dot row on it would say there was one.
func TestARunOfOneTaskOnTheRailWearsNoDots(t *testing.T) {
	a, rows := c266Rail(t, []session.PlanTaskRow{
		{ID: "solo", Title: "migrate the ledger", Status: "running", Total: 1},
	}, 120, true)
	_, run := c266RowWith(t, rows, "migrate the ledger")
	if strings.Contains(run, "/") {
		t.Fatalf("a one-task run's row carries a count:\n%s", run)
	}
	for _, cell := range []tokens.GlyphID{tokens.GDoneCell, tokens.GFailedCell, tokens.GEmptyCell} {
		if strings.Contains(run, plain(a.pal.glyph(cell))) {
			t.Fatalf("a one-task run's row carries dot cells:\n%s", run)
		}
	}
}

// ON A NARROW RAIL A HELD ROW KEEPS ITS NAME. The tail is a sentence and the
// rail is under thirty cells: laid first it left the title one letter, a row
// naming neither the task nor what it waits on.
func TestANarrowRailsHeldRowKeepsItsTitle(t *testing.T) {
	rows := c266PlanRows()
	rows[2].Title = "write the middleware"
	_, rail := c266Rail(t, rows, 150, false)
	_, held := c266RowWith(t, rail, "write the te")
	if !strings.Contains(held, "write the tests") {
		t.Fatalf("the held row lost its title to its tail:\n%s", held)
	}
	if strings.Contains(held, "w…") {
		t.Fatalf("the held row's title is one letter:\n%s", held)
	}
}

// ON THE RAIL A TASK UNDER A TASK IS DRAWN UNDER IT. The page's indent budget at
// the rail's width is one level, which drew a grandchild at its parent's indent:
// two siblings to the eye, in the one place the run's tree is read.
func TestTheRailIndentsATaskUnderItsParentTask(t *testing.T) {
	rows := c266PlanRows()
	rows = append(rows, session.PlanTaskRow{ID: "kid", Parent: "held", Title: "write the fixtures", Status: "pending"})
	// The widened column, where both titles are drawn whole beside the wait.
	_, rail := c266Rail(t, rows, 160, true)
	_, parent := c266RowWith(t, rail, "write the tests")
	_, child := c266RowWith(t, rail, "write the fi")
	if strings.Index(child, "write") <= strings.Index(parent, "write") {
		t.Fatalf("the task under a task is not indented under it:\n%s\n%s", parent, child)
	}
}

// A TASK UNDER A TASK STANDS A LEVEL IN, AND THE LINE A RUNNING PART WOULD
// HAVE HAD UNDER IT IS THE HINT'S. The family's connectors are gone from the
// side column (DESIGN.md, One side column): every task is one line, and a part
// shows its depth by its indent alone.
func TestTheFamilysLineRunsThroughTheLinesUnderARow(t *testing.T) {
	rows := c266PlanRows()
	rows = append(rows, session.PlanTaskRow{ID: "kid", Parent: "held", Title: "write the fixtures", Status: "pending"})
	rows = append(rows, session.PlanTaskRow{ID: "after", Parent: "root", Title: "update the manual", Status: "pending"})
	_, rail := c266Rail(t, rows, 150, false)
	at, handler := c266RowWith(t, rail, "write the handler")
	if at+1 < len(rail) && strings.Contains(plain(rail[at+1]), "$ git grep") {
		t.Fatalf("a live line stands under a one-line task:\n%s\n%s", handler, plain(rail[at+1]))
	}
	_, held := c266RowWith(t, rail, "write the tests")
	_, kid := c266RowWith(t, rail, "write the fi")
	if strings.Index(kid, "write the fi") <= strings.Index(held, "write the tests") {
		t.Fatalf("the task under a task is not a level in:\n%s\n%s", held, kid)
	}
}
