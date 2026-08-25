package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── A LONG LIST'S TAIL FADES WITH DEPTH — NEVER STRIPES ─────────────────────
//
// These are the whole of the claim (depthfade.go): the last rows of a window
// that has cut a list off step down the fade ladder, the head of that window
// does not, the row the cursor is on never does wherever it lands, and a list
// short enough to have its own last row on screen draws with no fade in it at
// all.

// fadeInk is the escape one stop of the ladder opens with, taken from the
// palette rather than written out — a test that spelled the sequence itself
// would be asserting the palette's arithmetic twice.
func fadeInk(pal palette, stop int) string {
	painted := pal.fade("\x00", stop)
	if at := strings.IndexByte(painted, 0); at > 0 {
		return painted[:at]
	}
	return ""
}

// fadeStopOf is which stop of the ladder a drawn line is wearing, or -1 for a
// line at full ink.
func fadeStopOf(pal palette, line string) int {
	for stop := 0; stop < fadeSteps; stop++ {
		if ink := fadeInk(pal, stop); ink != "" && strings.Contains(line, ink) {
			return stop
		}
	}
	return -1
}

// noFadeAnywhere fails when any line of a frame carries any stop of the ladder.
func noFadeAnywhere(t *testing.T, pal palette, lines []string, what string) {
	t.Helper()
	for i, line := range lines {
		if stop := fadeStopOf(pal, line); stop >= 0 {
			t.Fatalf("%s: row %d is faded at stop %d and nothing here should fade:\n%s",
				what, i, stop, strings.Join(lines, "\n"))
		}
	}
}

// ── the arithmetic ──────────────────────────────────────────────────────────

// THE LADDER IS THREE STEPS AND THE DEEPEST ROW IS THE FAINTEST. The stop is the
// depth, which is the one thing about [tailStop] somebody could reasonably
// "correct" into being backwards.
func TestTheDepthLadderIsThreeStepsAndTheLastRowIsTheFaintest(t *testing.T) {
	const rows = 20
	for at, want := range map[int]int{19: 0, 18: 1, 17: 2, 16: -1, 0: -1} {
		if got := tailStop(at, rows, true); got != want {
			t.Fatalf("row %d of %d took stop %d, want %d", at, rows, got, want)
		}
	}
	// AND A WINDOW WITH ITS LIST'S LAST ROW ON SCREEN FADES NOTHING.
	for at := 0; at < rows; at++ {
		if got := tailStop(at, rows, false); got != -1 {
			t.Fatalf("row %d faded at stop %d in a window nothing runs past", at, got)
		}
	}
	// AND A WINDOW WITH NO HEAD TO FADE AWAY FROM FADES NOTHING EITHER: three
	// dimmed rows out of four is a dimmed list, not a gradient.
	for rows := 1; rows < fadeFloor; rows++ {
		for at := 0; at < rows; at++ {
			if got := tailStop(at, rows, true); got != -1 {
				t.Fatalf("a %d-row window faded its row %d at stop %d", rows, at, got)
			}
		}
	}
}

// THE FADE REPAINTS THE ROW AND KEEPS EVERYTHING THAT IS NOT PAINT. A row this
// deep speaks in one voice, but an OSC 8 anchor is not a voice — it is where a
// word points, it occupies no cells, and a fade that ate it would quietly unlink
// the bottom of every list.
func TestTheDepthFadeRepaintsTheRowAndKeepsItsHyperlink(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	row := "\x1b]8;;file:///tmp/x\x07" + pal.accent("x.go") + "\x1b]8;;\x07 " + pal.dim("2m")

	faded := pal.fadeRow(row, 1)
	if strings.Contains(faded, pal.accent("x.go")) {
		t.Fatalf("the row kept its own ink through the fade:\n%q", faded)
	}
	if !strings.Contains(faded, "\x1b]8;;file:///tmp/x\x07") {
		t.Fatalf("the fade ate the row's hyperlink:\n%q", faded)
	}
	if got := plain(faded); got != "x.go 2m" {
		t.Fatalf("the fade changed the row's text to %q", got)
	}
	if stop := fadeStopOf(pal, faded); stop != 1 {
		t.Fatalf("the row came back at stop %d, want 1", stop)
	}
	// AND A ROW ASKED FOR NO STOP COMES BACK UNTOUCHED, byte for byte. That is
	// the emptiness law standing behind every surface below.
	if same := pal.fadeRow(row, -1); same != row {
		t.Fatalf("a row asked for no fade came back changed:\n%q", same)
	}
}

// ── /history ────────────────────────────────────────────────────────────────

// historyApp is a task page with `count` rows of the project's record behind it,
// which is more than any terminal in this suite is tall.
func historyApp(t *testing.T, count int) *app {
	t.Helper()
	a, _, _ := taskApp(t)
	a.width, a.height = 100, 24
	rows := make([]session.TaskIndexEntry, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, pastTask(
			"past-"+itoa(i), "task-"+itoa(i), "The "+fadeWord(i)+" errand", time.Duration(i+1)*time.Hour))
	}
	a.comp.tasks = rows
	if !a.openTaskSheet() {
		t.Fatal("/history refused to open on a project with a record")
	}
	return a
}

// fadeWord keeps the titles distinguishable without dragging in a word list.
func fadeWord(i int) string { return strings.Repeat("i", i%7+1) + itoa(i) }

// historyRows is the place's list region: the lines between the head the router
// draws ([placeHeadRows]) and the foot it draws under the body — a blank, a
// rule, the place's own note, the composer and the hint.
func historyRows(a *app) []string {
	width, height := a.size()
	lines, _, _, _ := a.taskSheetFrame(width, height)
	// The foot the router draws is five rows: a blank, a rule, the place's own
	// note, the composer and the hint. A filter adds a second note row.
	foot := 5
	if a.taskSheetFiltering() {
		foot++
	}
	return lines[placeHeadRows : len(lines)-foot]
}

// THE TAIL OF THE RECORD FADES AND ITS HEAD DOES NOT. A page holding two hundred
// errands is sharp where the reader is and quiet where they are not.
func TestTheHistoryPagesTailFadesWithDepthAndItsHeadDoesNot(t *testing.T) {
	a := historyApp(t, 200)
	rows := historyRows(a)
	if len(rows) < fadeFloor {
		t.Fatalf("the page's list region is only %d rows", len(rows))
	}

	// The last three step DOWN the ladder: the deepest row is the faintest.
	//
	// A BLANK LINE IS EXEMPT AND SAYS SO. The place separates its sections with
	// whitespace, so the foot of a window can land on one — and a blank line has
	// no ink to fade, which is the emptiness law rather than a hole in the ladder.
	lit := 0
	for depth := 0; depth < fadeSteps; depth++ {
		at := len(rows) - 1 - depth
		if strings.TrimSpace(plain(rows[at])) == "" {
			continue
		}
		lit++
		if stop := fadeStopOf(a.pal, rows[at]); stop != depth {
			t.Fatalf("row %d (depth %d) came back at stop %d, want %d:\n%s",
				at, depth, stop, depth, strings.Join(rows, "\n"))
		}
	}
	if lit == 0 {
		t.Fatalf("the last %d rows of the window are all blank:\n%s", fadeSteps, strings.Join(rows, "\n"))
	}
	// And nothing above them is faded at all.
	noFadeAnywhere(t, a.pal, rows[:len(rows)-fadeSteps], "the head of the record")
}

// A RECORD THAT FITS DRAWS EXACTLY AS IT ALWAYS DID. There is nothing under the
// last row to point at, so there is no gradient — not a faint one, none.
func TestAShortHistoryPageDrawsWithNoFadeAtAll(t *testing.T) {
	a := historyApp(t, 4)
	width, height := a.size()
	lines, _, _, _ := a.taskSheetFrame(width, height)
	noFadeAnywhere(t, a.pal, lines, "a four-row record")
}

// THE ROW THE CURSOR IS ON IS NEVER FADED, and the place that proves it is the
// bottom of the window — the cursor walked all the way down sits exactly where
// the ladder would otherwise be.
func TestTheHistoryPageNeverFadesTheRowTheCursorIsOn(t *testing.T) {
	a := historyApp(t, 200)
	// Walk the cursor to the foot of the window and past it, so it is the last
	// drawn row of a list that still runs on underneath.
	for i := 0; i < 60; i++ {
		a.taskSheetMove(1)
	}
	rows := historyRows(a)
	current, ok := a.taskSheetCurrent()
	if !ok || current.entry.Title == "" {
		t.Fatal("the cursor is not standing on a row of the record")
	}
	title := current.entry.Title
	found := false
	for i, row := range rows {
		if !strings.Contains(plain(row), title) {
			continue
		}
		found = true
		if stop := fadeStopOf(a.pal, row); stop >= 0 {
			t.Fatalf("the cursor's own row %d is faded at stop %d:\n%s",
				i, stop, strings.Join(rows, "\n"))
		}
	}
	if !found {
		t.Fatalf("the cursor's row %q is not on the page:\n%s", title, strings.Join(rows, "\n"))
	}
}

// THE PAGE'S SECTIONS ARE SEPARATED BY A BLANK LINE AND BY NOTHING ELSE. No
// rule, no dashes, no alternating background — the whitespace rhythm the column
// already uses between its own two sections (margin.go).
func TestTheHistoryPageSeparatesItsSectionsWithABlankLine(t *testing.T) {
	a, _, _ := taskApp(t)
	a.width, a.height = 100, 40
	railRun(a)
	a.comp.tasks = []session.TaskIndexEntry{
		pastTask("past-1", "one", "An errand from before", time.Hour),
	}
	if !a.openTaskSheet() {
		t.Fatal("/history refused to open")
	}
	rows := historyRows(a)
	head := -1
	for i, row := range rows {
		if strings.TrimSpace(plain(row)) == taskSheetNowHead {
			head = i
		}
	}
	if head <= 0 {
		t.Fatalf("a section word is not on the page:\n%s", strings.Join(rows, "\n"))
	}
	if got := strings.TrimSpace(plain(rows[head-1])); got != "" {
		t.Fatalf("the line above a section's word is %q, want a blank line:\n%s",
			got, strings.Join(rows, "\n"))
	}
	// AND ONE BLANK LINE AND NOT TWO: the breath between sections is a rhythm, and
	// a double gap reads as a section that lost its rows.
	if head >= 2 {
		if got := strings.TrimSpace(plain(rows[head-2])); got == "" {
			t.Fatalf("two blank lines stand above a section's word:\n%s", strings.Join(rows, "\n"))
		}
	}
	// AND THE PAGE OPENS ON WHAT IT IS HOLDING rather than on air: a blank line
	// under the router's own rule would be the head of the page drifting away
	// from it.
	if got := strings.TrimSpace(plain(rows[0])); !strings.HasPrefix(got, "work aforge ran on its own.") {
		t.Fatalf("the page opens on %q rather than on what it is holding", got)
	}
}

// ── the roster ──────────────────────────────────────────────────────────────

// fadeColumnApp is a column with more work in it than the frame is tall.
func fadeColumnApp(t *testing.T, count int) *app {
	t.Helper()
	a, _, _ := taskApp(t)
	a.width, a.height = 100, 24
	for i := 0; i < count; i++ {
		a.taskUpdate(update(uint64(i+1), "The "+fadeWord(i)+" errand", session.TaskDone, session.TaskNotice{}))
	}
	a.paints = 0
	return a
}

// THE COLUMN'S TAIL FADES INTO THE FOLD. It is the surface's longest list and the
// one most often cut off by nothing but a terminal's height.
func TestTheRostersTailFadesWithDepthAndItsHeadDoesNot(t *testing.T) {
	a := fadeColumnApp(t, 120)
	view, _ := a.railView(a.height)
	body := 0
	for _, line := range view {
		if line.fade > 0 {
			body++
		}
	}
	if body != fadeSteps {
		t.Fatalf("the column faded %d lines, want %d", body, fadeSteps)
	}
	rows := a.railRows(a.height)
	deep := -1
	for i, line := range view {
		if line.fade == 1 {
			deep = i
		}
	}
	if deep < 0 {
		t.Fatalf("no line of the column took the faintest stop")
	}
	for depth := 0; depth < fadeSteps; depth++ {
		at := deep - depth
		if stop := fadeStopOf(a.pal, rows[at]); stop != depth {
			t.Fatalf("column row %d (depth %d) came back at stop %d, want %d",
				at, depth, stop, depth)
		}
	}
	noFadeAnywhere(t, a.pal, rows[:deep-fadeSteps+1], "the head of the column")
}

// A COLUMN THAT FITS FADES NOTHING. Three tasks in a twenty-four-row terminal is
// a list with its own last row on screen.
func TestAShortRosterDrawsWithNoFadeAtAll(t *testing.T) {
	a := fadeColumnApp(t, 3)
	noFadeAnywhere(t, a.pal, a.railRows(a.height), "a three-task column")
}

// THE FOCUSED ROW IS NEVER FADED, wherever the cursor has walked it — including
// into the last three lines before the fold.
func TestTheRosterNeverFadesTheRowItsCursorIsOn(t *testing.T) {
	a := fadeColumnApp(t, 120)
	a.railTake(true)
	for i := 0; i < 200; i++ {
		a.railMove(1)
	}
	view, focus := a.railView(a.height)
	if focus < 0 {
		t.Fatal("the roster has no focused entry after walking its cursor down")
	}
	rows := a.railRows(a.height)
	stood := false
	for i, line := range view {
		if line.entry != focus {
			continue
		}
		stood = true
		if stop := fadeStopOf(a.pal, rows[i]); stop >= 0 {
			t.Fatalf("the focused row %d is faded at stop %d", i, stop)
		}
	}
	if !stood {
		t.Fatal("the focused entry is not on the column at all")
	}
}

// ── home ────────────────────────────────────────────────────────────────────

// THE LEFT COLUMN OF HOME IS AN INDEX, and an index that runs past its window
// fades into the fold like every other list on this surface.
func TestHomesListFadesItsTailOnlyWhenItRunsPastTheWindow(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", now)
	for i := 1; i < 40; i++ {
		lab.session("-tmp-alpha", "aaaa0000000000"+padTwo(i+1), "errand "+itoa(i), "/tmp/alpha",
			now.Add(-time.Duration(i)*time.Minute))
	}
	a := lab.app(mine)
	a.width, a.height = 100, 24
	openHomeOn(a, mine)
	// A project folds its quiet tail behind one `…more` row, which is home
	// refusing to be long in the first place. The subject here is what happens
	// when it IS long, so the tail is opened and the cursor put back on the row
	// the test arrived on.
	for _, line := range a.home.lines {
		if line.kind == homeQuiet {
			a.home.fold(line.dir, true)
			break
		}
	}
	a.home.point(mine)

	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	stops := map[int]int{}
	for _, line := range lines {
		if stop := fadeStopOf(a.pal, line); stop >= 0 {
			stops[stop]++
		}
	}
	for stop := 0; stop < fadeSteps; stop++ {
		if stops[stop] != 1 {
			t.Fatalf("home drew %d rows at stop %d, want exactly one:\n%s",
				stops[stop], stop, plain(strings.Join(lines, "\n")))
		}
	}

	// AND A HOME THAT FITS FADES NOTHING.
	small := newHomeLab(t)
	only := small.session("-tmp-alpha", "aaaa000000000001", "one errand", "/tmp/alpha", now)
	b := small.app(only)
	b.width, b.height = 100, 24
	openHomeOn(b, only)
	width, height = b.size()
	short, _, _, _ := b.homeFrame(width, height)
	noFadeAnywhere(t, b.pal, short, "a one-conversation home")
}

// padTwo keeps the lab's session ids the same length, which is what makes their
// order on disk the order the test wrote them in.
func padTwo(i int) string {
	if i < 10 {
		return "0" + itoa(i)
	}
	return itoa(i)
}
