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
	tree := tasksTreeOf(c253PlanItems(rows, "chat"), now, tasksSort{})
	var got []string
	for _, group := range tree.groups {
		if len(group.roots) > 0 {
			got = append(got, group.roots[0].entry.Title)
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
	tree := tasksTreeOf(c253PlanItems(rows, "chat"), taskFixtureNow, tasksSort{})
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

func TestPlanRailFoldsDoneRowsToOneCountAtFamilyBottom(t *testing.T) {
	rows := []session.PlanTaskRow{
		{ID: "root", Title: "family", Status: "running"},
		{ID: "done-a", Parent: "root", Title: "done A", Status: "done"},
		{ID: "live", Parent: "root", Title: "live child", Status: "running"},
		{ID: "done-b", Parent: "root", Title: "done B", Status: "done"},
		{ID: "queued", Parent: "root", Title: "queued child", Status: "ready"},
	}
	items := c253PlanItems(rows, "chat")
	reading := tasksReading{items: items, held: len(items), now: taskFixtureNow}
	lines := reading.lay(55)
	var titles []string
	var folded *tasksLine
	for i := range lines {
		if lines[i].kind != tasksLineTask {
			continue
		}
		titles = append(titles, lines[i].item.entry.Title)
		if lines[i].item.entry.Activity == "2 done" {
			folded = &lines[i]
		}
	}
	if strings.Contains(strings.Join(titles, "|"), "done A") || strings.Contains(strings.Join(titles, "|"), "done B") {
		t.Fatalf("done rows were drawn separately: %v", titles)
	}
	if folded == nil || titles[len(titles)-1] != folded.item.entry.Title {
		t.Fatalf("done fold is not one counted task line at the family bottom: titles=%v fold=%v", titles, folded)
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
