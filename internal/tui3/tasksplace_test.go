package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func tasksFixture() (session.World, session.UsageWindow, time.Time) {
	loc := time.FixedZone("fixture", -4*60*60)
	now := time.Date(2026, time.August, 25, 13, 11, 0, 0, loc)
	at := func(day, hour int) time.Time { return time.Date(2026, time.August, day, hour, 0, 0, 0, loc) }
	entry := func(id, label, status string, ended time.Time, cost float64) session.TaskIndexEntry {
		return session.TaskIndexEntry{ID: id, Label: label, Status: status, EndedAt: ended, Cost: cost}
	}
	first := session.SessionRow{ID: "room-a", Title: "swarm splitting", Project: "aforge", Open: true}
	first.Tasks.Rows = []session.TaskIndexEntry{
		entry("1", "verify the pro model's pricing", string(session.TaskUnverified), at(25, 11), .40),
		entry("2", "read 40 filings", string(session.TaskRunning), time.Time{}, .92),
		entry("3", "toy-scale validation", string(session.TaskDone), at(25, 10), 1.63),
		entry("4", "install the render toolchain", string(session.TaskFailed), at(25, 4), .05),
	}
	first.Tasks.Rows[1].Activity = "18 of 40"
	first.Tasks.Rows[1].Kind = session.TaskKindAdaptive
	first.Tasks.Rows[2].FilesChanged = 1
	first.Tasks.Rows[2].Outcome = "a report"
	first.Tasks.Rows[3].Outcome = "the package manager refused the archive"

	second := session.SessionRow{ID: "room-b", Title: "thor clips", Project: "media", Open: true}
	second.Tasks.Rows = []session.TaskIndexEntry{
		entry("5", "render fight clip", string(session.TaskQueued), time.Time{}, 3.10),
		entry("6", "summarise loud movers", string(session.TaskDone), at(25, 6), .31),
	}
	for i := 0; i < 8; i++ {
		cost := 1.0
		if i == 7 {
			cost = 20.69
		}
		second.Tasks.Rows = append(second.Tasks.Rows,
			entry(string(rune('a'+i)), "earlier task "+itoa(i+1), string(session.TaskDone), at(24-i, 9), cost))
	}
	world := session.World{Projects: []session.Project{
		{Name: "aforge", Sessions: []session.SessionRow{first}},
		{Name: "media", Sessions: []session.SessionRow{second}},
	}, Read: now}
	return world, session.LastDays(now, 24), now
}

func TestTheTasksPageGroupsByWhatYouDoNext(t *testing.T) {
	world, win, now := tasksFixture()
	reading := readTasks(world, win, now.Add(-time.Hour), now)
	rows := reading.rows(120, newPalette(tokens.NoColor, false))
	page := strings.Join(rows, "\n")
	wants := []string{"needs your look", "running", "done today", "earlier"}
	last := -1
	for _, want := range wants {
		at := strings.Index(page, want)
		if at < 0 || at <= last {
			t.Fatalf("section %q is absent or out of order in:\n%s", want, page)
		}
		last = at
	}
	if !strings.Contains(page, "work aforge ran on its own. 14 since aug 2, $34.10 of it.") {
		t.Fatalf("header did not count the window and its known spend:\n%s", page)
	}
	if !strings.Contains(page, tokens.GlyphNeedsHuman+" verify the pro model's pricing") {
		t.Fatalf("the row needing a look did not wear %q:\n%s", tokens.GlyphNeedsHuman, page)
	}
	if strings.Count(page, "adaptive") != 1 || !strings.Contains(page, "read 40 filings") {
		t.Fatalf("the kind word did not stay on the adaptive row alone:\n%s", page)
	}
	if !strings.Contains(page, tokens.GlyphCollapsed+" 2 more, back to aug 2") {
		t.Fatalf("the earlier fold did not state its count and window edge:\n%s", page)
	}
}

func TestTheTasksPageDrawsNoEmptySection(t *testing.T) {
	world, win, now := tasksFixture()
	world.Projects[0].Sessions[0].Tasks.Rows = world.Projects[0].Sessions[0].Tasks.Rows[:1]
	world.Projects = world.Projects[:1]
	page := strings.Join(readTasks(world, win, time.Time{}, now).rows(100, newPalette(tokens.NoColor, false)), "\n")
	if strings.Contains(page, "\nrunning\n") || strings.Contains(page, "\ndone today\n") || strings.Contains(page, "\nearlier\n") {
		t.Fatalf("an empty section drew a heading:\n%s", page)
	}
}

func TestEveryTasksRowKeepsInsideItsCells(t *testing.T) {
	world, win, now := tasksFixture()
	reading := readTasks(world, win, time.Time{}, now)
	for _, width := range []int{60, 80, 120, 200} {
		for i, row := range reading.rows(width, newPalette(tokens.TrueColor, false)) {
			if got := ansi.StringWidth(row); got > width {
				t.Errorf("row %d drew %d cells at width %d: %q", i, got, width, plain(row))
			}
		}
	}
}

func TestTheTasksCursorOnlyOpensTaskRows(t *testing.T) {
	world, win, now := tasksFixture()
	reading := readTasks(world, win, time.Time{}, now)
	rows := reading.rows(120, newPalette(tokens.NoColor, false))
	found := 0
	for i, row := range rows {
		entry, room, ok := reading.at(i)
		isTask := strings.Contains(row, "verify the pro") || strings.Contains(row, "read 40 filings") ||
			strings.Contains(row, "toy-scale") || strings.Contains(row, "install the render") ||
			strings.Contains(row, "render fight") || strings.Contains(row, "summarise loud") ||
			strings.Contains(row, "earlier task")
		if ok != isTask {
			t.Fatalf("row %d mapped=%v, task-row=%v: %q", i, ok, isTask, row)
		}
		if ok {
			found++
			if entry.ID == "" || room.ID == "" {
				t.Fatalf("row %d lost its entry or room: %+v / %+v", i, entry, room)
			}
		}
	}
	if found != 12 { // Six state/today rows and the six visible earlier rows.
		t.Fatalf("mapped %d task rows, want 12", found)
	}
}

func TestTheTasksWindowUsesTheSharedArrowGrammar(t *testing.T) {
	_, win, now := tasksFixture()
	r := tasksReading{}
	tests := map[string]session.UsageWindow{
		"shift+left":  win.Step(-1),
		"shift+right": win.Step(1),
		"shift+up":    win.Coarser(),
		"shift+down":  win.Finer(),
		"x":           win,
	}
	for key, want := range tests {
		if got := r.step(win, key); got != want {
			t.Errorf("%s answered %+v, want %+v at %v", key, got, want, now)
		}
	}
}

func TestTheEmptyTasksPlaceTeachesWithoutInventingRows(t *testing.T) {
	pal := newPalette(tokens.NoColor, false)
	if got := tasksTeach(pal); len(got) != 3 || !strings.Contains(strings.Join(got, "\n"), "enter opens") {
		t.Fatalf("teaching rows = %#v", got)
	}
	if rows := readTasks(session.World{}, session.UsageWindow{}, time.Time{}, time.Time{}).rows(80, pal); len(rows) != 0 {
		t.Fatalf("an empty reading drew %#v", rows)
	}
}
