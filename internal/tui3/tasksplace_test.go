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
	if !strings.Contains(page, "work aforge ran on its own. 14 pieces of work, $34.10 between them.") {
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

// A FAMILY IS ONE PIECE OF WORK even when its members are in different states.
// The whole tree stands in the most urgent member's section, so a refused child
// stays visibly under its running parent with the check's concrete reason.
func TestARunningParentKeepsItsRefusedChildUnderIt(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	rows := world.Projects[0].Sessions[0].Tasks.Rows
	rows[0].Status, rows[0].EndedAt = string(session.TaskRunning), time.Time{}
	rows[1].Status = string(session.TaskFailed)
	rows[1].Ending = session.TaskEndingRefused
	rows[1].Outcome = "incomplete — the corrected diff stayed on the child branch"
	world.Projects[0].Sessions[0].Open = true

	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	for _, item := range reading.items {
		if tasksFamilyOf(item.entry) == tasksFamilyOf(rows[0]) && item.section != tasksRunning {
			t.Fatalf("family member %q was filed under %q, want running", item.entry.Label, tasksSectionWord(item.section))
		}
	}
	reading.open = map[tasksKey]bool{tasksFamilyOf(rows[0]): true}
	found := false
	for _, line := range reading.lay(120) {
		if line.kind == tasksLineTask && line.item.entry.ID == "2" {
			found = true
			if line.kin != tasksKinCont && line.kin != tasksKinLast {
				t.Fatalf("the refused worker is no longer under its parent: %q", line.kin)
			}
			if got := taskStateWord(line.item.entry, line.item.runs); got != taskRecordStoppedWord {
				t.Fatalf("refused child state = %q, want %q", got, taskRecordStoppedWord)
			}
			if got := tasksMiddle(line.item.entry); got != rows[1].Outcome {
				t.Fatalf("refused child reason = %q, want %q", got, rows[1].Outcome)
			}
		}
	}
	if !found {
		t.Fatal("the refused child is not on the page at all")
	}
}

// A ROOT OUTSIDE THE WINDOW CANNOT HOLD A FOLD. Its visible children stand on
// their own and keep their own sections; one running sibling must not drag an
// older sibling under running when the parent row is not there to group them.
func TestVisibleChildrenKeepTheirOwnSectionsWhenTheRootIsAbsent(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	rows := world.Projects[0].Sessions[0].Tasks.Rows
	world.Projects[0].Sessions[0].Tasks.Rows = rows[1:4]
	world.Projects[0].Sessions[0].Tasks.Rows[0].Status = string(session.TaskRunning)
	world.Projects[0].Sessions[0].Tasks.Rows[0].EndedAt = time.Time{}
	world.Projects[0].Sessions[0].Open = true

	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	sections := map[string]tasksSection{}
	for _, item := range reading.items {
		sections[item.entry.ID] = item.section
	}
	if sections["2"] != tasksRunning {
		t.Fatalf("running orphan section = %q, want running", tasksSectionWord(sections["2"]))
	}
	if sections["3"] != tasksToday {
		t.Fatalf("landed orphan section = %q, want today", tasksSectionWord(sections["3"]))
	}
	for _, line := range reading.lay(120) {
		if line.kind == tasksLineTask && (line.kin == tasksKinCont || line.kin == tasksKinLast) {
			t.Fatalf("orphan child %q was drawn under an absent root", line.item.entry.Label)
		}
	}
}

// ── ONE ROW, LAID OUT BY WHAT IT IS FOR ─────────────────────────────────────
//
// These are the polish wave's rows against this place: the name kept whole, the
// facts separated and ranked, the sections in the order their headings promise,
// and a head sentence that counts the PLACE rather than the query.

// tasksPolishFixture is one conversation's three finished pieces of work,
// spelled the way the demo home spells them — long names, a long sentence of
// detail beside each — and arriving OUT of time order, which is what the world
// scan hands this place.
func tasksPolishFixture() (session.World, session.UsageWindow, time.Time) {
	loc := time.FixedZone("fixture", -4*60*60)
	now := time.Date(2026, time.September, 2, 23, 3, 0, 0, loc)
	done := func(id, label, outcome string, files int, cost float64, ago time.Duration) session.TaskIndexEntry {
		return session.TaskIndexEntry{
			ID: id, Label: label, Title: label, SessionID: "room-a",
			Status: string(session.TaskDone), EndedAt: now.Add(-ago),
			FilesChanged: files, Outcome: outcome, Cost: cost,
		}
	}
	row := session.SessionRow{ID: "room-a", Title: "The Annual Toggle", Project: "aforge", Open: true}
	row.Tasks.Rows = []session.TaskIndexEntry{
		done("1", "Put the annual toggle on the pricing page",
			"Annual is the default and the monthly price stays visible beside it.", 2, .27, 5*time.Hour),
		done("2", "Port the picker onto the new list",
			"The picker walks the list's rows and keeps its own cursor; the keys are unchanged.", 2, .31, 45*time.Minute),
		done("3", "Write the morning sweep down",
			"The sweep is a standing order now, with a cap of a dollar a day.", 1, .14, 2*time.Hour),
	}
	world := session.World{Projects: []session.Project{
		{Name: "aforge", Sessions: []session.SessionRow{row}},
	}, Read: now}
	return world, session.LastDays(now, 14), now
}

// tasksPage is the whole page as a person reads it, with no colour on it.
func tasksPage(r tasksReading, width int) string {
	rows := r.rows(width, newPalette(tokens.NoColor, false))
	for i := range rows {
		rows[i] = plain(rows[i])
	}
	return strings.Join(rows, "\n")
}

// tasksRowFor finds the drawn row a name is on. It is deliberately a search of
// the PAINTED page rather than of the reading: what is on the screen is the
// thing under test.
func tasksDrawnRow(page, name string) string {
	for _, line := range strings.Split(page, "\n") {
		if strings.Contains(line, name) {
			return strings.TrimRight(line, " ")
		}
	}
	return ""
}

// THE IDENTITY IS WHOLE OR THE ROW IS POINTLESS (rowfit.go, law 1). The list
// exists to let a person MATCH NAMES, and `✓ Put the…` matches nothing: the row
// used to hand every fact its full spelling first and give the name whatever
// was left, so at a hundred and sixty columns — the most room anybody has —
// three rows in four still cut the name.
func TestTheTaskNameIsWholeBeforeAnyFactGetsACell(t *testing.T) {
	world, win, now := tasksPolishFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	for _, width := range []int{60, 80, 120, 160} {
		page := tasksPage(reading, width)
		for _, item := range reading.items {
			name := tasksLabel(item.entry)
			if !strings.Contains(page, name) {
				t.Fatalf("at %d columns the page drew\n%s\nand no row on it carries the whole name %q", width, page, name)
			}
		}
	}
	// AND THE OTHER HALF OF LAW 1: a name that had to be cut takes the whole
	// row, because a row that has already spent the one thing it was drawn to
	// say may not spend cells on a figure as well.
	item := reading.items[0]
	narrow := plain(tasksRow(tasksLine{kind: tasksLineTask, item: item}, 30, now, newPalette(tokens.NoColor, false)))
	if strings.Contains(narrow, "$") || strings.Contains(narrow, rowSep) {
		t.Fatalf("a frame too narrow for the name alone drew\n  %s\nwant the cut name and no facts beside it", narrow)
	}
}

// THE FACTS ARE JOINED BY ` · ` AND RANKED. `The Certificate Rotation
// incomplete now` was three separate facts a reader had to re-parse into three,
// and one row mixed a real ` · ` INSIDE a fact with bare spaces BETWEEN facts,
// so the only separator on the row meant two different things.
func TestTheFactsOnATaskRowAreJoinedByOneSeparator(t *testing.T) {
	world, win, now := tasksPolishFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	const name = "Put the annual toggle on the pricing page"
	// AND THEY DEGRADE BY SPELLING RATHER THAN BY ENDING THE TAIL (law 2): the
	// detail sentence is seventy-seven cells at its longest and seven at its
	// shortest, and each width says the longest it has room for.
	for _, want := range []struct {
		width int
		tail  string
	}{
		{160, "5h · $0.27 · The Annual Toggle · 2 files · Annual is the default and the monthly price stays visible beside it."},
		{120, "5h · $0.27 · The Annual Toggle · 2 files"},
		{80, "5h · $0.27 · The Annual Toggle"},
	} {
		row := tasksDrawnRow(tasksPage(reading, want.width), name)
		if !strings.HasSuffix(row, want.tail) {
			t.Fatalf("at %d columns the row reads\n  %s\nand it should end on\n  … %s", want.width, row, want.tail)
		}
	}
}

// A SECTION NAMED BY TIME IS ORDERED BY IT. Under `done today` the ages used to
// read `2h, 50m, 5h, 5h`, because the rows arrived in the world scan's order —
// projects sorted by when somebody was last in one of their CONVERSATIONS,
// which is a fact about conversations and not about work.
func TestEachTasksSectionReadsNewestFirst(t *testing.T) {
	world, win, now := tasksPolishFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	want := []string{
		"Port the picker onto the new list",
		"Write the morning sweep down",
		"Put the annual toggle on the pricing page",
	}
	var got []string
	for _, line := range reading.lay(120) {
		if line.kind == tasksLineTask {
			got = append(got, tasksLabel(line.item.entry))
		}
	}
	if strings.Join(got, " / ") != strings.Join(want, " / ") {
		t.Fatalf("`done today` reads\n  %s\nand it should read, newest first\n  %s",
			strings.Join(got, " / "), strings.Join(want, " / "))
	}
	// AND A FAMILY MOVES AS ONE, placed by its root's own stamp with the workers
	// under it in their own order.
	fam, famWin, famNow := tasksFamilyFixture()
	family := readTasks(fam, tasksMine{}, famWin, time.Time{}, famNow)
	family.open = map[tasksKey]bool{{session: "room-a", id: "1"}: true}
	var kin []string
	for _, line := range family.lay(120) {
		if line.kind == tasksLineTask {
			kin = append(kin, line.kin)
		}
	}
	if strings.Join(kin, "") != tasksFoldOpen+tasksKinCont+tasksKinCont+tasksKinLast+tasksKinPad {
		t.Fatalf("an opened family came out as %q, and the three workers must stand together under their root", kin)
	}
}

// THE HEAD SENTENCE IS A CLAIM ABOUT THE PLACE AND THE FILTER IS A PROPERTY OF
// THE QUERY. Typing a word nothing matches used to draw `work aforge ran on its
// own. nothing.` across the top of a machine that had run ten pieces of work.
func TestAFilterThatMatchesNothingStillCountsThePlace(t *testing.T) {
	world, win, now := tasksPolishFixture()
	a := &app{pal: newPalette(tokens.NoColor, false)}
	a.clock = func() time.Time { return now }
	a.raisePlace(pageTasks)
	a.taskSheet.world = world
	a.taskSheet.reading = readTasks(world, tasksMine{}, win, time.Time{}, now)
	a.taskSheet.query.setText("zzz")

	r := a.tasksFiltered()
	if len(r.items) != 0 {
		t.Fatalf("the query kept %d rows and it should have emptied the list", len(r.items))
	}
	got := r.head(120, false)
	want := "work aforge ran on its own. 3 pieces of work, $0.72 between them."
	if got != want {
		t.Fatalf("a query nothing matches makes the page say\n  %s\nand what is true of the machine is\n  %s", got, want)
	}
}

// THE FIGURE GETS ITS NOUN, and the shut fold says what it is holding in words.
// `10,` is a number a person has to guess the unit of; `+3 under` is arithmetic
// with a preposition for a noun.
func TestTheTasksHeadAndItsFoldsSayTheirFiguresInWords(t *testing.T) {
	world, win, now := tasksPolishFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	if got, want := reading.head(120, false), "work aforge ran on its own. 3 pieces of work, $0.72 between them."; got != want {
		t.Fatalf("the head reads\n  %s\nwant\n  %s", got, want)
	}
	// THE WINDOW'S EDGE IS CARRIED WHERE THE CONTROL IS NOT DRAWN, and the spend
	// clause is what a frame too narrow for the whole sentence gives up — never
	// a figure cut in half.
	edge := reading.head(80, true)
	if !strings.Contains(edge, " pieces of work since ") || !strings.HasSuffix(edge, "$0.72 between them.") {
		t.Fatalf("the narrow head reads\n  %s\nwant `… N pieces of work since <date>, $0.72 between them.`", edge)
	}
	if got := reading.head(60, true); strings.Contains(got, "$") || !strings.HasSuffix(got, ".") {
		t.Fatalf("at sixty cells the head reads\n  %s\nwant the sentence without its spend clause", got)
	}
	// ONE piece of work is one piece of work.
	one := reading
	one.whole, one.wholeCost = 1, 0.27
	if got, want := one.head(120, false), "work aforge ran on its own. 1 piece of work, $0.27 of it."; got != want {
		t.Fatalf("one row makes the head read\n  %s\nwant\n  %s", got, want)
	}
	if got, want := tasksUnderWord(3), "holds 3 more"; got != want {
		t.Fatalf("a shut fold says %q, want %q", got, want)
	}
	if got, want := tasksUnderWord(1), "holds 1 more"; got != want {
		t.Fatalf("a fold holding one says %q, want %q", got, want)
	}
	// AND IT IS THE FIRST THING THE TAIL SAYS, joined like every other fact.
	// `3 files holds 3 more` ran two claims together with a bare space, at the
	// far right of the row, eleven cells from the edge where the eye does not go.
	fam, famWin, famNow := tasksFamilyFixture()
	shut := readTasks(fam, tasksMine{}, famWin, time.Time{}, famNow)
	row := tasksDrawnRow(tasksPage(shut, 120), "port the parser")
	if !strings.Contains(row, "holds 3 more · ") {
		t.Fatalf("the shut family's row reads\n  %s\nand its count must lead the tail: `… holds 3 more · <facts>`", row)
	}
}

// A ROW NOBODY IS RUNNING IS NOT DATED `now`. The row said in one half that the
// window running it is gone and in the other that it is happening this second,
// and the age is the half a person believes. Nothing in the record dates a row
// that never landed, so the honest answer is no age at all.
func TestARowNobodyIsRunningIsNotDatedNow(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	stalled := session.TaskIndexEntry{
		ID: "3", Label: "Rotate the wildcard certificate", Title: "Rotate the wildcard certificate",
		Status: string(session.TaskRunning), SessionID: "a-window-that-went",
	}
	reading := readTasks(session.World{}, tasksMine{rows: []tasksMineRow{{entry: stalled}}},
		session.LastDays(now, 10), time.Time{}, now)
	row := tasksDrawnRow(tasksPage(reading, 120), "Rotate the wildcard certificate")
	if !strings.HasSuffix(row, taskRecordStoppedWord) {
		t.Fatalf("a row nobody is behind reads\n  %s\nand it should end on\n  … %s, with no age after it", row, taskRecordStoppedWord)
	}
}

// A CHILD DOES NOT REPEAT ITS PARENT'S CONVERSATION. Opening a family drew the
// root's title once per row down the page, spending twenty-odd cells to restate
// a fact the row two lines up had already stated — on rows whose names were
// being cut to make room for it.
func TestAnOpenedFamilyNamesItsConversationOnce(t *testing.T) {
	world, win, now := tasksFamilyFixture()
	reading := readTasks(world, tasksMine{}, win, time.Time{}, now)
	reading.open = map[tasksKey]bool{{session: "room-a", id: "1"}: true}
	page := tasksPage(reading, 120)
	if got := strings.Count(page, "the split"); got != 2 {
		t.Fatalf("the conversation is named %d times on\n%s\nwant once per root and never on a worker", got, page)
	}
	if !strings.Contains(page, "port the lexer") {
		t.Fatalf("the opened family drew no workers:\n%s", page)
	}
}

// ONE STATE WORD ON BOTH SURFACES. The list read `gave up, said why` where the
// page one keypress away read `failed`, and a person who filed the row under one
// of those words could not find it under the other.
func TestTheListAndThePageSayOneWordAboutWorkThatFailed(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	entry := session.TaskIndexEntry{
		ID: "4", Label: "Rebuild the frame budget report", Title: "Rebuild the frame budget report",
		SessionID: "room-a", Status: string(session.TaskFailed), Cost: .18,
		EndedAt: now.AddDate(0, 0, -8), Outcome: "the package manager refused the archive",
	}
	word := taskStateWord(entry, false)
	if got := tasksMiddle(entry); !strings.HasPrefix(got, word+rowSep) {
		t.Fatalf("the list says\n  %s\nand its own page says\n  %s\nwhich must be the first word of both", got, word)
	}
	a := &app{pal: newPalette(tokens.NoColor, false)}
	a.clock = func() time.Time { return now }
	a.raisePlace(pageTasks)
	if got := a.taskCardWhenLine(entry); !strings.HasPrefix(got, word) {
		t.Fatalf("the page opens on\n  %s\nand the list says\n  %s", got, word)
	}
	// AND WORK THAT FAILED DID NOT LAND. `landed` is this codebase's word for
	// work that ARRIVED.
	if got := a.taskCardWhenLine(entry); !strings.Contains(got, "stopped 8d ago") || strings.Contains(got, "landed") {
		t.Fatalf("the failed task's page reads\n  %s\nwant `%s · stopped 8d ago …`", got, word)
	}
}

// THE PAGE IS A SUPERSET OF THE ROW IT OPENED FROM. The row carries the
// conversation the work came out of; the card — the surface a person opens
// precisely to learn more — used to drop it, so the one fact tying the record to
// a conversation was available only on the row they had just left.
func TestTheTaskPageNamesTheConversationItCameOutOf(t *testing.T) {
	world, _, now := tasksPolishFixture()
	entry := world.Projects[0].Sessions[0].Tasks.Rows[0]
	a := &app{pal: newPalette(tokens.NoColor, false)}
	a.clock = func() time.Time { return now }
	a.raisePlace(pageTasks)
	a.taskSheet.world = world
	body := plain(strings.Join(a.taskCardBody(entry, 90), "\n"))
	if !strings.Contains(body, "out of The Annual Toggle") {
		t.Fatalf("the card reads\n%s\nand the row it opened from says the work came out of %q", body, "The Annual Toggle")
	}
}
