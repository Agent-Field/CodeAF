package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func c253PlanItems(rows []session.PlanTaskRow, chat string) []tasksItem {
	kin := planKinOf(rows)
	out := make([]tasksItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, planItem(row, chat, kin))
	}
	return out
}

func TestPlanRailOrdersFamiliesByWorkThenNewestActivity(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	rows := []session.PlanTaskRow{
		{ID: "done-old", Title: "done old", Status: "done", Ended: now.Add(-2 * time.Hour)},
		{ID: "queued", Title: "queued family", Status: "pending", Started: now.Add(-time.Minute)},
		{ID: "running-old", Title: "running old", Status: "running", Started: now.Add(-time.Hour)},
		{ID: "done-new", Title: "done new", Status: "done", Ended: now.Add(-10 * time.Minute)},
		{ID: "running-new", Title: "running new", Status: "claimed", Started: now.Add(-time.Minute)},
	}
	tree := (tasksReading{items: c253PlanItems(rows, "chat"), now: now}).railTree()
	var got []string
	for _, group := range tree.groups {
		for _, root := range group.roots {
			got = append(got, root.entry.Title)
		}
	}
	want := []string{"running new", "running old", "queued family", "done new", "done old"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("family order = %v, want %v", got, want)
	}
}

func TestPlanRailKeepsStoreOrderInsideFamilyExceptRunningFloatsTop(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "root", Title: "family", Status: "running"},
		{ID: "queued-a", Parent: "root", Title: "queued A", Status: "pending"},
		{ID: "done-a", Parent: "root", Title: "done A", Status: "done"},
		{ID: "running", Parent: "root", Title: "running", Status: "claimed"},
		{ID: "queued-b", Parent: "root", Title: "queued B", Status: "ready"},
		{ID: "done-b", Parent: "root", Title: "done B", Status: "done"},
	}
	tree := (tasksReading{items: c253PlanItems(rows, "chat"), now: taskFixtureNow}).railTree()
	kids := tree.kids[tasksKey{session: "chat", id: "root"}]
	var got []string
	for _, kid := range kids {
		got = append(got, kid.entry.Title)
	}
	want := []string{"running", "queued A", "done A", "queued B", "done B"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("inside-family order = %v, want running first then store order %v", got, want)
	}
}

func TestPlanRailRunRowWearsFiveProgressCells(t *testing.T) {
	root := session.PlanTaskRow{ID: "root", Title: "run", Status: "running", Done: 3, Running: 1, Queued: 6, Total: 10}
	item := planItem(root, "chat", planKinOf([]session.PlanTaskRow{root}))
	item.section = tasksRunning
	reading := tasksReading{items: []tasksItem{item}, held: 1, now: taskFixtureNow}
	lines := reading.lay(50)
	var painted []string
	for i := range lines {
		painted = append(painted, reading.paint(lines, i, 50, palette{}, false))
	}
	text := strings.Join(painted, "\n")
	cells := 0
	for _, id := range []tokens.GlyphID{tokens.GDoneCell, tokens.GRunningCell, tokens.GEmptyCell, tokens.GFailedCell} {
		cells += strings.Count(text, palette{}.glyph(id))
	}
	cells-- // the row state mark is the same slot as a running progress cell
	if cells != 5 {
		t.Fatalf("rail run row has %d progress cells, want 5:\n%s", cells, text)
	}
}

func TestTheRailDoesNotReorderTheSessionsTree(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "old", Title: "older running work", Status: "running", Started: taskFixtureNow.Add(-time.Hour)},
		{ID: "new", Title: "newer completed work", Status: "done", Ended: taskFixtureNow.Add(-time.Minute)},
	}
	items := c253PlanItems(rows, "chat")
	tree := tasksTreeOf(items, taskFixtureNow, tasksSort{})
	reading := tasksReading{items: items, held: len(items), now: taskFixtureNow, shape: &tree}
	before := strings.Join(reading.rows(140, palette{}), "\n")
	rail := reading.planRailForest(rows)
	if len(rail) < 2 || rail[0].row.ID != "old" {
		t.Fatalf("the compact rail lost running-first order: %v", rail)
	}
	if after := strings.Join(reading.rows(140, palette{}), "\n"); after != before {
		t.Fatalf("drawing the rail reordered the cached Sessions page:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if strings.Index(before, "newer completed work") > strings.Index(before, "older running work") {
		t.Fatal("the Sessions page did not retain newest-first order")
	}
}
