package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The rail's two sections, as the chat side builds them: what a conversation row
// carries, what hangs under a job card, and where a click on either one lands.

// A thread row carries the four facts the switcher's row carries, because they
// are one list read two ways: the name, the line it was left at, when it last
// moved, and the ornament.
func TestAThreadRowCarriesWhatTheSwitcherRowCarries(t *testing.T) {
	app, _ := threadRailApp(t)
	row := railRow(t, app, rowRoomPrefix+"session-two")

	if row.Kind != rail.RowThread {
		t.Fatalf("a conversation is drawn as %v", row.Kind)
	}
	if row.Name != "importer rewrite" {
		t.Fatalf("the row is named %q", row.Name)
	}
	if !strings.Contains(row.Status, "schema") {
		t.Fatalf("the left-at line reads %q", row.Status)
	}
	if row.When == "" {
		t.Fatal("the row carries no relative time")
	}

	// And the room you are standing in says so instead, because `you are here`
	// outranks where you left off — you did not leave.
	here := railRow(t, app, rowRoomPrefix+testSession)
	if here.Status != "you are here" {
		t.Fatalf("the current room says %q", here.Status)
	}
}

// DOT DISCIPLINE, both halves. A window that has never opened a thread dots
// nothing at all — a fresh window that lit every row would be announcing that
// the product is new rather than that anything happened — and the row the reader
// is standing in never dots, whatever lands in it.
func TestTheDotIsOnlyEverAnUnseenDelivery(t *testing.T) {
	app, _ := threadRailApp(t)
	for _, row := range app.railModel.Rows() {
		if row.Unseen {
			t.Fatalf("a first-run window dotted %q", row.Name)
		}
	}

	// Now the window has been in that thread, and something newer has landed.
	app.noteThreadSeen("session-two", fixedNow().Add(-40*time.Hour))
	app.reopenRail()
	if !railRow(t, app, rowRoomPrefix+"session-two").Unseen {
		t.Fatal("a delivery that landed after this window last looked is not dotted")
	}
	if railRow(t, app, rowRoomPrefix+testSession).Unseen {
		t.Fatal("the room the reader is standing in is dotted")
	}
	// Which is what the collapsed handle is asking, one scope at a time.
	if !app.railModel.Scope().Unseen() {
		t.Fatal("the handle would carry no dot with an unseen delivery on the rail")
	}
}

// The work section is a REAL TREE, two levels deep, and the depth below that is
// a count rather than a third indent.
func TestALiveJobsPartsHangUnderItsCard(t *testing.T) {
	app, _ := boardApp(t)
	rows := app.railModel.Rows()

	limbs := 0
	for _, row := range rows {
		if !row.Tree {
			continue
		}
		limbs++
		if row.Depth != 1 {
			t.Fatalf("the limb %q sits at depth %d, want the tree's one level", row.Name, row.Depth)
		}
	}
	if limbs == 0 {
		t.Fatal("the live job drew no parts at all")
	}

	// A SETTLED JOB IS ONE CARD. History is a receipt, and eight rows of it
	// would push live work off the bottom of the column.
	for i, row := range rows {
		if row.ID != rowTaskPrefix+"job-2" {
			continue
		}
		if i+1 < len(rows) && rows[i+1].Tree {
			t.Fatalf("the settled job expanded into %q", rows[i+1].Name)
		}
	}
}

// A click on a conversation walks into it, and a click on a work row opens that
// job's room. The pointer reaches the same calls the keyboard does — that is
// what makes click parity a fact rather than a promise — so what is asserted
// here is where each of the two kinds of row LANDS.
func TestClickingAThreadRowAndAWorkRowLandWhereTheyPointed(t *testing.T) {
	app, _ := threadRailApp(t)
	at := rowIndexOf(t, app, rowRoomPrefix+"session-two")
	if cmd := app.scopePoint(railPoint{row: at}); cmd != nil {
		cmd()
	}
	if app.session != "session-two" {
		t.Fatalf("clicking a conversation left the window in %q", app.session)
	}

	app, _ = boardApp(t)
	at = rowIndexOf(t, app, rowTaskPrefix+"job-1")
	if cmd := app.scopePoint(railPoint{row: at}); cmd != nil {
		cmd()
	}
	if app.view == nil || app.view.kind != viewNode || app.view.node != "job-1" {
		t.Fatalf("clicking a job did not open its room: %+v", app.view)
	}

	// And a LIMB opens the part's own room, which is the whole point of drawing
	// it: a row the reader can see is a row the reader can reach. It is read off
	// a FRESH window, because the click above descended into the job and the
	// limbs of an entered plan belong to that scope rather than to home.
	app, _ = boardApp(t)
	limb := ""
	for _, row := range app.railModel.Rows() {
		if row.Tree {
			limb = row.ID
			break
		}
	}
	if limb == "" {
		t.Fatal("the fixture drew no limb to click")
	}
	if cmd := app.scopePoint(railPoint{row: rowIndexOf(t, app, limb)}); cmd != nil {
		cmd()
	}
	if app.view == nil || app.view.kind != viewNode ||
		app.view.node != strings.TrimPrefix(limb, rowTaskPrefix) {
		t.Fatalf("clicking a limb did not open the part's room: %+v", app.view)
	}
}

// SPATIAL STABILITY, and its barrier. While the column is open a row that is
// already on screen never moves, whatever order the source now returns; the
// resort happens when the rail comes BACK, before the reader's eye has landed
// anywhere.
func TestRowsHoldStillWhileOpenAndResortOnTheWayBack(t *testing.T) {
	app, backend := boardApp(t)
	before := jobOrder(app)
	if len(before) < 2 {
		t.Fatalf("the fixture has %d jobs, so nothing can move", len(before))
	}

	// The source re-sorts under the reader: the older job is commissioned again
	// and floats to the top of the board's own newest-first ordering.
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-2" {
			backend.nodes[i].CreatedSeq = 9999
		}
	}
	app.source.refresh(1, true)
	app.railModel.Refresh()
	if got := jobOrder(app); !sameOrder(got, before) {
		t.Fatalf("the rail re-sorted under an open column: %q, was %q", got, before)
	}

	// Away and back. The order is taken fresh at the moment the column returns.
	app.setRail(railStateOf("slim"))
	app.setRail(railStateOf("open"))
	if got := jobOrder(app); sameOrder(got, before) {
		t.Fatalf("re-opening the rail kept an order nobody was looking at: %q", got)
	}
}

// jobOrder is the job cards on the rail, in the order they are drawn.
func jobOrder(app *App) []string {
	var out []string
	for _, row := range app.railModel.Rows() {
		if row.Kind == rail.RowTask {
			out = append(out, row.Name)
		}
	}
	return out
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// railRow is one row of the home rail by id.
func railRow(t *testing.T, app *App, id string) rail.Row {
	t.Helper()
	return app.railModel.Rows()[rowIndexOf(t, app, id)]
}

func rowIndexOf(t *testing.T, app *App, id string) int {
	t.Helper()
	for i, row := range app.railModel.Rows() {
		if row.ID == id {
			return i
		}
	}
	t.Fatalf("no row with id %q on the rail: %q", id, rowNames(app))
	return -1
}

// threadRailApp is a window over the chats fixture: two named conversations with
// a line in each, so the threads section has left-at lines to draw.
func threadRailApp(t *testing.T) (*App, *threadsBackend) {
	t.Helper()
	backend := threadsGoldenBackend()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app, backend
}
