package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

func c263PlanRows() []session.PlanTaskRow {
	live := livePlanRow()
	live.ID, live.Parent, live.Title = "live", "root", "implement handler"
	return []session.PlanTaskRow{
		{ID: "root", Title: "rewrite the auth", Status: "running", Done: 2, Running: 1, Queued: 1, Total: 4},
		live,
		{ID: "gate", Parent: "root", Title: "schema migration", Status: "running"},
		{ID: "queued", Parent: "root", Title: "integration tests", Status: "pending", Waits: []string{"gate"}},
		{ID: "done-a", Parent: "root", Title: "old fixture", Status: "done"},
		{ID: "done-b", Parent: "root", Title: "old helper", Status: "done"},
	}
}

func TestRailPlanUsesTasksReadingTree(t *testing.T) {
	a, _ := planAppWith(t, c263PlanRows(), nil)
	a.width, a.height, a.railWide = 120, 30, true
	if !openTaskPlaceWithRows(a) {
		t.Fatal("tasks place did not read the plan")
	}
	room := a.railRoom()
	wantRows := a.tasksFiltered().planRows(room, a.pal)
	if len(wantRows) == 0 {
		t.Fatal("tasks reading returned no plan tree")
	}
	want := plain(strings.Join(wantRows, "\n"))
	got := plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
	for _, word := range []string{"rewrite the auth", "implement handler", "$ git grep", "queued · waits: schema migration", "2 done", "2/4"} {
		if !strings.Contains(want, word) {
			t.Fatalf("tasks reading lacks %q:\n%s", word, want)
		}
		if !strings.Contains(got, word) {
			t.Fatalf("rail lacks tasks-reading word %q:\n%s", word, got)
		}
	}
	for _, row := range a.railRows(a.viewHeight()) {
		if ansi.StringWidth(row) > a.railWidth() {
			t.Fatalf("rail row is %d cells in a %d-cell rail: %q", ansi.StringWidth(row), a.railWidth(), row)
		}
	}
}

func TestRailPlanProjectionLeavesNoPlanReadingUnchanged(t *testing.T) {
	a, _ := planAppWith(t, nil, nil)
	a.showPage(pageTasks)
	reading := a.tasksFiltered()
	before := reading.rows(50, a.pal)
	if got := reading.planRows(50, a.pal); got != nil {
		t.Fatalf("no-plan projection = %#v, want nil", got)
	}
	after := reading.rows(50, a.pal)
	if strings.Join(before, "\x00") != strings.Join(after, "\x00") {
		t.Fatalf("asking for a plan projection changed the legacy reading\nbefore=%q\nafter=%q", before, after)
	}
}

// THE RAIL DRAWS THE TREE IN A CONVERSATION NOBODY HAS OPENED THE TASKS PLACE
// IN. The person sits in the chat; the tasks place is a room they may never walk
// into, and a rail that waited for that walk would show no tree in the one
// place the owner asked for it.
func TestRailPlanDrawsWithoutTheTasksPlaceEverOpening(t *testing.T) {
	a, _ := planAppWith(t, c263PlanRows(), nil)
	a.width, a.height, a.railWide = 120, 30, true
	// The paint clock's own beat, which is what moves the stamp the reading's
	// freshness hangs on ([app.refreshElsewhere], [tasksPlace.regroup]).
	a.refreshElsewhere()
	got := plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
	for _, word := range []string{"rewrite the auth", "implement handler", "schema migration", "2/4"} {
		if !strings.Contains(got, word) {
			t.Fatalf("the rail of a conversation that never opened the tasks place lacks %q:\n%s", word, got)
		}
	}
}
