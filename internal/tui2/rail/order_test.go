package rail

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// §6's ordering law, both halves, on the merge that decides it.
//
// THE DEFECT: "rail seems to be adding tasks to bottom instead of top down".
// The source has always handed this list newest-first; the merge appended every
// unseen row to the end of the whole scope, so the newest job landed under the
// oldest one AND under the collapsed group at the foot of the rail.
//
// The law is two sentences that only look like one: a row ALREADY VISIBLE never
// moves relative to another visible row, and a row nobody has seen yet goes
// where the source put it. These tests are written so that satisfying one by
// breaking the other cannot pass.

// homeWith replaces the home scope's rows and returns the source.
func homeWith(src *fakeSource, rows []Row) *fakeSource {
	home := src.scopes[HomeScopeID]
	home.Rows = rows
	src.set(home)
	return src
}

// taskRow is one job card as the source draws it.
func taskRow(id, name string) Row {
	return Row{ID: id, Kind: RowTask, Name: name, Life: LifeWorking, Composer: ComposerChat}
}

// TestANewTaskLandsOnTopOfTheSectionItBelongsTo is the reported defect, stated
// as the frame the reader looks at: the job just commissioned is the first job
// on the rail, not the last row of it.
//
// The fixture keeps a row BELOW the task section (the collapsed group the real
// rail carries) precisely because that is where the appended row used to go —
// a test whose fixture ended at the task section could not see the bug.
func TestANewTaskLandsOnTopOfTheSectionItBelongsTo(t *testing.T) {
	src := scene()
	group := Row{ID: "homes", Kind: RowStep, Name: "everything else"}
	homeWith(src, []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow(idWisp, "wisp-parity"),
		taskRow(idClean, "data-clean"),
		group,
	})
	m := New(src)
	if got := names(m.Rows()); !equal(got,
		[]string{"aforge", "wisp-parity", "data-clean", "everything else"}) {
		t.Fatalf("the fixture opened as %v", got)
	}

	// The source is newest-first, so a job commissioned now is drawn above every
	// job already on the rail.
	homeWith(src, []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow("brand-new", "just asked"),
		taskRow(idWisp, "wisp-parity"),
		taskRow(idClean, "data-clean"),
		group,
	})
	m.Refresh()
	want := []string{"aforge", "just asked", "wisp-parity", "data-clean", "everything else"}
	if got := names(m.Rows()); !equal(got, want) {
		t.Fatalf("rows = %v, want %v — a new task must land on top of its section", got, want)
	}
	if got := names(m.Rows()); got[len(got)-1] != "everything else" {
		t.Fatalf("the new row sank past the collapsed group: %v", got)
	}
}

// TestTwoArrivalsInOneRefreshKeepTheSourcesOrderBetweenThem: newest-first is a
// total order, and a refresh that carries two new jobs must not invert them on
// the way in.
func TestTwoArrivalsInOneRefreshKeepTheSourcesOrderBetweenThem(t *testing.T) {
	src := scene()
	homeWith(src, []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow(idWisp, "wisp-parity"),
	})
	m := New(src)
	homeWith(src, []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow("newest", "third ask"),
		taskRow("newer", "second ask"),
		taskRow(idWisp, "wisp-parity"),
	})
	m.Refresh()
	want := []string{"aforge", "third ask", "second ask", "wisp-parity"}
	if got := names(m.Rows()); !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

// TestAVisibleRowNeverReordersWhileVisible is the other half, and it is the one
// the fix could plausibly have broken: the source re-sorting under the reader —
// because a job finished, because a badge appeared — moves nothing that is
// already drawn.
func TestAVisibleRowNeverReordersWhileVisible(t *testing.T) {
	src := scene()
	rows := []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow(idWisp, "wisp-parity"),
		taskRow(idClean, "data-clean"),
		taskRow(idPerf, "perf-audit"),
	}
	homeWith(src, rows)
	m := New(src)
	before := names(m.Rows())

	// Every possible re-sort of the same three visible jobs, plus a badge on the
	// one the source promoted — the case §6 is explicitly about.
	for _, order := range [][]int{{3, 2, 1}, {2, 1, 3}, {3, 1, 2}, {1, 3, 2}} {
		next := []Row{rows[0]}
		for _, i := range order {
			r := rows[i]
			r.Questions = 3
			next = append(next, r)
		}
		homeWith(src, next)
		m.Refresh()
		if got := names(m.Rows()); !equal(got, before) {
			t.Fatalf("source order %v moved the rail to %v, want %v", order, got, before)
		}
	}
}

// TestAnArrivalDoesNotDisturbTheRowsAroundIt: the splice inserts, it does not
// re-lay-out. Everything above and below the new row keeps its neighbours.
func TestAnArrivalDoesNotDisturbTheRowsAroundIt(t *testing.T) {
	src := scene()
	rows := []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow(idWisp, "wisp-parity"),
		taskRow(idClean, "data-clean"),
		{ID: "homes", Kind: RowStep, Name: "everything else"},
	}
	homeWith(src, rows)
	m := New(src)
	// The source now returns the two jobs the other way round AND a new one on
	// top: the re-sort must be ignored and the arrival must not be.
	homeWith(src, []Row{
		rows[0],
		taskRow("brand-new", "just asked"),
		rows[2],
		rows[1],
		rows[3],
	})
	m.Refresh()
	want := []string{"aforge", "just asked", "wisp-parity", "data-clean", "everything else"}
	if got := names(m.Rows()); !equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

// TestTheCursorStaysOnItsRowWhenSomethingArrivesAbove: the whole reason the
// stability law exists is that the reader is pointing at something. An insert
// above the cursor moves the row's index and must not move the selection.
func TestTheCursorStaysOnItsRowWhenSomethingArrivesAbove(t *testing.T) {
	src := scene()
	rows := []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow(idWisp, "wisp-parity"),
		taskRow(idClean, "data-clean"),
	}
	homeWith(src, rows)
	m := New(src)
	m.SelectID(idClean)
	homeWith(src, []Row{rows[0], taskRow("brand-new", "just asked"), rows[1], rows[2]})
	m.Refresh()
	if got := m.Selected().ID; got != idClean {
		t.Fatalf("the cursor drifted to %q when a row arrived above it", got)
	}
	if got := names(m.Rows())[1]; got != "just asked" {
		t.Fatalf("the arrival is at row %q, want it on top of the tasks", got)
	}
}

// TestAnArrivalWhoseAnchorRetiredInTheSameRefreshStillDraws: the row a new job
// was spliced above can vanish in the very read that carried the job. The job
// is still a job.
func TestAnArrivalWhoseAnchorRetiredInTheSameRefreshStillDraws(t *testing.T) {
	src := scene()
	homeWith(src, []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow(idWisp, "wisp-parity"),
	})
	m := New(src)
	homeWith(src, []Row{
		{ID: "home", Kind: RowSurface, Name: "aforge"},
		taskRow("brand-new", "just asked"),
	})
	m.Refresh()
	if got := names(m.Rows()); !equal(got, []string{"aforge", "just asked"}) {
		t.Fatalf("rows = %v, want the arrival to survive its anchor", got)
	}
}

// -- §20's grid, on the rail ---------------------------------------------------

// TestTheRailRowsAreOnTheGrid is the addendum's assertion in the one place the
// rail could drift from it: a gutter of two, a content edge at two, and a depth
// step of two.
//
// It reads the CONSTANTS as well as a frame, because the frame proves this
// build and the constants are what the next depth will be laid out from.
func TestTheRailRowsAreOnTheGrid(t *testing.T) {
	if gutterWidth+1 != 2 {
		t.Fatalf("the rail's gutter is %d cells with its space, want 2", gutterWidth+1)
	}
	if indentStep != 2 {
		t.Fatalf("the rail's indent step is %d, want 2", indentStep)
	}

	src := scene()
	m := New(src)
	view := NewView(tokens.NewStylerIn(tokens.NoColor, tokens.FocusNormal, tokens.Plain))
	rows := view.Render(m, ModeAuto, 40, 24)
	if len(rows) == 0 {
		t.Fatal("the rail drew nothing")
	}
	// The rail is an INSET surface, exactly as the composer is: column 0 is the
	// pane's own selection rail — §20 admits "selection rails and accent edges"
	// into the gutter as chrome — and §20's grid is measured from inside it. So
	// every row's ink opens a whole number of STEPS past that edge: at the
	// gutter's own glyph column, or at the content edge, or at a depth past it.
	// A row that opened one cell off would be the school-notebook drift §20
	// exists to stop.
	for i, row := range rows {
		plain := ansi.Strip(row)
		if len(plain) < gutterWidth {
			continue
		}
		inside := plain[gutterWidth:]
		trimmed := strings.TrimLeft(inside, " ")
		if trimmed == "" {
			continue
		}
		at := len(inside) - len(trimmed)
		if at%indentStep != 0 {
			t.Fatalf("row %d opens %d cells inside the pane's edge, off the %d-cell step: %q",
				i, at, indentStep, plain)
		}
	}
}
