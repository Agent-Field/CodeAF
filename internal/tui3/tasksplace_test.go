package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// tasksWidths is every width the reading is asked to hold at. It is the same
// sweep every reading in this package answers, because a row that fits at 120
// and spills at 60 is a row nobody checked at the width people actually use.
var tasksWidths = []int{60, 80, 120, 200}

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
	reading := readTasks(world, tasksMine{}, win, now.Add(-time.Hour), now)
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
	if !strings.Contains(page, "work aforge ran on its own. 14, $34.10 of it.") {
		t.Fatalf("header did not count the window and its known spend:\n%s", page)
	}
	// AND THE HEAD LINE IS THE WINDOW'S CONTROL TOO (SCREEN 3d), exactly as
	// standing and spend draw it. This place bound all four arrow keys and drew
	// nothing naming them for three waves.
	if !strings.Contains(page, "shift+← aug 2 – aug 25 →") {
		t.Fatalf("the tasks head line draws no window control:\n%s", page)
	}
	if !strings.Contains(page, tokens.GlyphNeedsHuman+" verify the pro model's pricing") {
		t.Fatalf("the row needing a look did not wear %q:\n%s", tokens.GlyphNeedsHuman, page)
	}
	if strings.Count(page, "adaptive") != 1 || !strings.Contains(page, "read 40 filings") {
		t.Fatalf("the kind word did not stay on the adaptive row alone:\n%s", page)
	}
	// THE WINDOW'S EDGE IS SAID ONCE, in the sentence the page opens on. It used
	// to be repeated on a fold at the foot of every section, which is one number
	// in four places and exactly the drift the one-source-of-truth law forbids.
	if n := strings.Count(page, "aug 2 "); n != 1 {
		t.Fatalf("the window's edge is spelled %d times, want once:\n%s", n, page)
	}
}

// NOTHING IS HIDDEN BEHIND A LINE NO KEY ANSWERS. Every section used to stop at
// six rows and append `▸ N more`, which on a record of two hundred was a fold
// standing in front of a hundred and ninety-four rows with no way through it —
// a capability that cannot work, which this codebase leaves off rather than
// draws broken. The place scrolls instead, so every row it holds has a line.
func TestNoTasksRowIsHiddenBehindAFold(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	row := session.SessionRow{ID: "many", Title: "many", Open: true}
	statuses := []string{string(session.TaskUnverified), string(session.TaskRunning), string(session.TaskDone), string(session.TaskDone)}
	for section, status := range statuses {
		for i := 0; i < 7+section; i++ {
			ended := now.AddDate(0, 0, -2)
			if section == 2 {
				ended = now.Add(-time.Hour)
			}
			row.Tasks.Rows = append(row.Tasks.Rows, session.TaskIndexEntry{
				ID:     fmt.Sprintf("%d-%d", section, i),
				Label:  fmt.Sprintf("section %d row %d", section, i),
				Status: status, EndedAt: ended,
			})
		}
	}
	reading := readTasks(session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}},
		tasksMine{}, session.LastDays(now, 10), time.Time{}, now)
	text := strings.Join(reading.rows(120, newPalette(tokens.NoColor, false)), "\n")
	if strings.Contains(text, tokens.GlyphCollapsed) {
		t.Fatalf("a section folded rows away behind a glyph no key opens:\n%s", text)
	}
	for section := range statuses {
		for i := 0; i < 7+section; i++ {
			want := fmt.Sprintf("section %d row %d", section, i)
			if !strings.Contains(text, want) {
				t.Fatalf("%q is on no line of the page:\n%s", want, text)
			}
		}
	}
	// And the layout says the same thing the paint does: one line per row of
	// work, at every width.
	for _, width := range tasksWidths {
		stops := 0
		for _, line := range reading.lay(width) {
			if line.kind == tasksLineTask {
				stops++
			}
		}
		if stops != len(reading.items) {
			t.Fatalf("at %d columns the layout carries %d rows of work, want %d", width, stops, len(reading.items))
		}
	}
}

func TestTheTasksPageDrawsNoEmptySection(t *testing.T) {
	world, win, now := tasksFixture()
	world.Projects[0].Sessions[0].Tasks.Rows = world.Projects[0].Sessions[0].Tasks.Rows[:1]
	world.Projects = world.Projects[:1]
	page := strings.Join(readTasks(world, tasksMine{}, win, time.Time{}, now).rows(100, newPalette(tokens.NoColor, false)), "\n")
	if strings.Contains(page, "\nrunning\n") || strings.Contains(page, "\ndone today\n") || strings.Contains(page, "\nearlier\n") {
		t.Fatalf("an empty section drew a heading:\n%s", page)
	}
}

func TestEveryTasksRowKeepsInsideItsCells(t *testing.T) {
	world, win, now := tasksFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	for _, width := range tasksWidths {
		for i, row := range reading.rows(width, newPalette(tokens.TrueColor, false)) {
			if got := ansi.StringWidth(row); got > width {
				t.Errorf("row %d drew %d cells at width %d: %q", i, got, width, plain(row))
			}
		}
	}
}

// THE SECTIONS ARE SEPARATED BY A BLANK LINE AND BY NOTHING ELSE — no rule, no
// dashes, no alternating background. It is the whitespace rhythm the column
// already keeps between its own two sections (margin.go).
func TestTheTasksSectionsAreSeparatedByABlankLineAndNothingElse(t *testing.T) {
	world, win, now := tasksFixture()
	lines := readTasks(world, tasksMine{}, win, time.Time{}, now).lay(120)
	if len(lines) == 0 {
		t.Fatal("the fixture laid out nothing")
	}
	// The page opens on its own sentence and never on air.
	if lines[0].kind != tasksLineWord || lines[0].text == "" {
		t.Fatalf("the page opens on %+v rather than on what it is holding", lines[0])
	}
	words := 0
	for i, line := range lines {
		if i == 0 || line.kind != tasksLineWord {
			continue
		}
		words++
		if lines[i-1].kind != tasksLineAir {
			t.Fatalf("the section word %q is not preceded by a blank line: %+v", line.text, lines[i-1])
		}
		if i >= 2 && lines[i-2].kind == tasksLineAir {
			t.Fatalf("the section word %q is preceded by two blank lines", line.text)
		}
	}
	if words != 4 {
		t.Fatalf("the fixture drew %d section words, want 4", words)
	}
}

func TestTheTasksCursorOnlyOpensTaskRows(t *testing.T) {
	world, win, now := tasksFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	pal := newPalette(tokens.NoColor, false)
	lines := reading.lay(120)
	rows := reading.rows(120, pal)
	if len(rows) != len(lines) {
		t.Fatalf("the paint drew %d lines and the layout laid out %d", len(rows), len(lines))
	}
	found := 0
	for i, row := range rows {
		item, ok := reading.at(lines, i)
		isTask := strings.Contains(row, "verify the pro") || strings.Contains(row, "read 40 filings") ||
			strings.Contains(row, "toy-scale") || strings.Contains(row, "install the render") ||
			strings.Contains(row, "render fight") || strings.Contains(row, "summarise loud") ||
			strings.Contains(row, "earlier task")
		if ok != isTask {
			t.Fatalf("row %d mapped=%v, task-row=%v: %q", i, ok, isTask, row)
		}
		if ok {
			found++
			if item.entry.ID == "" || item.row.ID == "" {
				t.Fatalf("row %d lost its entry or its conversation: %+v", i, item)
			}
		}
	}
	if found != len(reading.items) {
		t.Fatalf("mapped %d task rows, want %d", found, len(reading.items))
	}
}

// ONE PIECE OF WORK IS DRAWN ONCE, however many authorities know about it. The
// file, this window's own index and the window next door all describe the same
// row, and they are deduplicated on the pair internal/session says identifies
// one — the conversation that ran it and the id inside that conversation.
func TestOnePieceOfWorkIsDrawnOnceAcrossEveryAuthority(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	shared := session.TaskIndexEntry{
		ID: "3", Label: "Port the parser", Title: "Port the parser",
		Status: string(session.TaskRunning), SessionID: "the-other-window",
	}
	row := session.SessionRow{ID: "the-other-window", Title: "next door"}
	row.Tasks.Rows = []session.TaskIndexEntry{shared}
	world := session.World{Projects: []session.Project{{Sessions: []session.SessionRow{row}}}}
	mine := tasksMine{
		rows: []tasksMineRow{{entry: shared}},
		away: []session.ElsewhereTask{{
			SessionID: "the-other-window", Session: "docs pass",
			Task: session.PresenceTask{ID: "3", Title: "Port the parser", State: string(session.TaskRunning)},
		}},
	}
	reading := readTasks(world, mine, session.LastDays(now, 10), time.Time{}, now)
	if len(reading.items) != 1 {
		t.Fatalf("three authorities produced %d rows, want 1: %+v", len(reading.items), reading.items)
	}
	// THE FRESHEST AUTHORITY WINS. The window next door is reading a presence
	// file written seconds ago; the index file cannot correct itself.
	item := reading.items[0]
	if !item.away || !item.runs || item.window != "docs pass" {
		t.Fatalf("the freshest authority did not win: %+v", item)
	}
	if item.section != tasksRunning {
		t.Fatalf("work another window is holding is filed under %q", tasksSectionWord(item.section))
	}
	// AND IT TAKES NO CURSOR. There is no room here to open and nothing landed
	// for a mention to point at.
	lines := reading.lay(120)
	for i := range lines {
		if _, ok := reading.at(lines, i); ok {
			t.Fatalf("line %d offers a cursor over another window's work", i)
		}
	}
}

// A ROW THAT CLAIMS TO BE RUNNING WITH NOBODY BEHIND IT SAYS SO. Nothing rewrites
// a file when the window that wrote it dies, so the claim is judged rather than
// repeated — and the row lands where something true can be said about it.
func TestARowNobodyIsRunningSaysItIsIncomplete(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	stalled := session.TaskIndexEntry{
		ID: "3", Label: "Port the parser", Title: "Port the parser",
		Status: string(session.TaskRunning), SessionID: "a-window-that-went",
	}
	reading := readTasks(session.World{}, tasksMine{rows: []tasksMineRow{{entry: stalled}}},
		session.LastDays(now, 10), time.Time{}, now)
	if len(reading.items) != 1 || reading.items[0].section != tasksEarlier {
		t.Fatalf("a claim nobody is behind is filed as %+v", reading.items)
	}
	page := strings.Join(reading.rows(120, newPalette(tokens.NoColor, false)), "\n")
	if !strings.Contains(page, taskRecordStoppedWord) {
		t.Fatalf("the row does not say %q:\n%s", taskRecordStoppedWord, page)
	}
	if strings.Contains(page, taskAwayWord) {
		t.Fatalf("a row nobody is running is credited to a window:\n%s", page)
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
	if got := tasksTeach(pal); len(got) != 4 || !strings.Contains(strings.Join(got, "\n"), "enter opens") {
		t.Fatalf("teaching rows = %#v", got)
	}
	empty := readTasks(session.World{}, tasksMine{}, session.UsageWindow{}, time.Time{}, time.Time{})
	for _, width := range tasksWidths {
		if rows := empty.rows(width, pal); len(rows) != 0 {
			t.Fatalf("an empty reading drew %#v at %d columns", rows, width)
		}
		if got := empty.tally(); got != "" {
			t.Fatalf("an empty reading counted %q", got)
		}
	}
}

func TestTasksChangedSinceCountsOnlyLandedWorkAfterTheLook(t *testing.T) {
	seen := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	a := &app{}
	a.raisePlace(pageTasks)
	a.taskSheet.world = session.World{Projects: []session.Project{{Sessions: []session.SessionRow{{
		Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{
			{EndedAt: seen.Add(time.Minute)}, {EndedAt: seen.Add(-time.Minute)}, {},
		}},
	}}}}}
	if got := a.tasksChangedSince(seen); got != 1 {
		t.Fatalf("changed tasks = %d, want 1", got)
	}
}

// tasksFamilyFixture is one conversation that split a piece of work up: a root
// that landed today with three workers under it, and a second root beside it
// that is nobody's family.
func tasksFamilyFixture() (session.World, session.UsageWindow, time.Time) {
	loc := time.FixedZone("fixture", -4*60*60)
	now := time.Date(2026, time.August, 25, 13, 11, 0, 0, loc)
	at := func(hour int) time.Time { return time.Date(2026, time.August, 25, hour, 0, 0, 0, loc) }
	row := session.SessionRow{ID: "room-a", Title: "the split", Project: "aforge", Open: true}
	kid := func(id, parent, label string) session.TaskIndexEntry {
		return session.TaskIndexEntry{
			ID: id, Parent: parent, Label: label, SessionID: "room-a",
			Status: string(session.TaskDone), EndedAt: at(10),
		}
	}
	row.Tasks.Rows = []session.TaskIndexEntry{
		kid("1", "", "port the parser"),
		kid("2", "1", "port the lexer"),
		kid("3", "1", "port the tests"),
		kid("4", "1", "port the docs"),
		kid("9", "", "rename the flag"),
	}
	world := session.World{Projects: []session.Project{
		{Name: "aforge", Sessions: []session.SessionRow{row}},
	}, Read: now}
	return world, session.LastDays(now, 24), now
}

// TestTheTasksPageFoldsAFamilyShutAndOpensItOnDemand is the clutter fix: eight
// workers used to arrive as eight peers of everything else this machine ran.
func TestTheTasksPageFoldsAFamilyShutAndOpensItOnDemand(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)

	shut := reading.lay(120)
	work := func(lines []tasksLine) []tasksLine {
		var out []tasksLine
		for _, line := range lines {
			if line.kind == tasksLineTask {
				out = append(out, line)
			}
		}
		return out
	}
	rows := work(shut)
	// TWO ROWS AND NOT FIVE: the root, and the task that heads no family.
	if len(rows) != 2 {
		t.Fatalf("a shut page drew %d rows of work, and it holds one family and one loner", len(rows))
	}
	root := rows[0]
	if !root.folds || root.kids != 3 || root.open {
		t.Fatalf("the root came out as folds=%v kids=%d open=%v", root.folds, root.kids, root.open)
	}
	if root.kin != tasksFoldShut {
		t.Fatalf("the shut root wears %q", root.kin)
	}
	// AND THE LONER HOLDS THE COLUMN OPEN rather than sitting two cells left of
	// everything else.
	if rows[1].kin != tasksKinPad {
		t.Fatalf("the task with no family wears %q", rows[1].kin)
	}
	// THE SHUT FOLD SAYS WHAT IS UNDER IT.
	page := strings.Join(reading.rows(120, newPalette(tokens.NoColor, false)), "\n")
	if !strings.Contains(page, tasksUnderWord(3)) {
		t.Fatalf("the shut family does not say what it is holding:\n%s", page)
	}
	if strings.Contains(page, "port the lexer") {
		t.Fatalf("a child was drawn under a shut fold:\n%s", page)
	}

	// OPENED, the three workers are under it, connected, and the last one closes
	// the family.
	reading.open = map[tasksKey]bool{tasksFamilyOf(root.item.entry): true}
	rows = work(reading.lay(120))
	if len(rows) != 5 {
		t.Fatalf("an open family drew %d rows of work", len(rows))
	}
	if rows[0].kin != tasksFoldOpen {
		t.Fatalf("the open root wears %q", rows[0].kin)
	}
	if rows[1].kin != tasksKinCont || rows[3].kin != tasksKinLast {
		t.Fatalf("the connectors came out as %q … %q", rows[1].kin, rows[3].kin)
	}
	if !strings.Contains(strings.Join(reading.rows(120, newPalette(tokens.NoColor, false)), "\n"), "port the lexer") {
		t.Fatal("an open family does not draw its children")
	}
}

// TestAPageWithNoFamiliesDrawsNoFamilyColumn is the other half of the law: the
// column APPEARS when there is a tree, so nothing moves sideways on a machine
// that has never split work up.
func TestAPageWithNoFamiliesDrawsNoFamilyColumn(t *testing.T) {
	world, win, now := tasksFixture()
	for _, line := range readTasks(world, tasksMine{}, win, time.Time{}, now).lay(120) {
		if line.kind == tasksLineTask && line.kin != "" {
			t.Fatalf("a page with no families drew the column: %q on %q", line.kin, line.item.entry.Label)
		}
	}
}

// TestAChildWhoseRootIsNotOnThisSectionStandsAlone guards the row that would
// otherwise vanish: the sections are what a person acts on next, so a worker
// still running under a root that landed this morning is filed apart from it.
func TestAChildWhoseRootIsNotOnThisSectionStandsAlone(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	rows := world.Projects[0].Sessions[0].Tasks.Rows
	rows[2].Status, rows[2].EndedAt = string(session.TaskRunning), time.Time{}
	world.Projects[0].Sessions[0].Open = true

	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	found := false
	for _, line := range reading.lay(120) {
		if line.kind == tasksLineTask && line.item.entry.ID == "3" {
			found = true
			if line.kin == tasksKinCont || line.kin == tasksKinLast {
				t.Fatalf("the running worker was drawn as a child of a root in another section: %q", line.kin)
			}
		}
	}
	if !found {
		t.Fatal("the running worker is not on the page at all")
	}
}
