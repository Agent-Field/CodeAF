package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE ONE MARKED HEADING ──────────────────────────────────────────────────
//
// The row under the cursor says which ROW. The heading over it says which
// REGION (homesection.go holds the whole law). These tests pin the four facts
// that make it worth a rung: it follows the keyboard, it names the section that
// owns the cursor whatever kind of section that is, there is never more than one
// of it, and there is none of it at rest or under a search.
//
// THE SECTIONS ARE THE GRID'S PANELS NOW (homegrid.go), and the mark is the
// one docs/DESIGN-LANGUAGE.md states: the heading of the panel the cursor is
// standing in wears the cursor step's ground, its words stay muted, and one
// heading per frame wears it ([homeView.marksPanel]).

// cursorGround is the escape sequence THE GROUND LADDER's cursor step opens
// with, asked of the painter itself ([palette.cursor]) so this file names a STEP
// rather than a hex value the ladder is free to retune.
func cursorGround(pal palette) string {
	lead, _, _ := strings.Cut(pal.cursor(" ", 1), " ")
	return lead
}

// markedHeadings is every panel heading on the built grid the frame is
// MARKING, named by its word — a slice rather than a single value so "exactly
// one" is a thing a test can see fail.
func markedHeadings(a *app) []string {
	var out []string
	if !a.home.gridOn() {
		return out
	}
	for at, line := range a.home.lines {
		if a.home.marksPanel(at) {
			out = append(out, line.cell.title)
		}
	}
	return out
}

// standInPanel puts the cursor on the first row of one panel and answers the
// line it landed on.
func standInPanel(t *testing.T, a *app, panel homePanelID) int {
	t.Helper()
	for at, line := range a.home.lines {
		if line.stop() && line.cell != nil && line.cell.panel == panel {
			a.home.cursor = at
			return at
		}
	}
	t.Fatalf("panel %d has no row to stand on:\n%s", panel, homeText(a))
	return homeNoLine
}

// sectionLab is a machine with two projects, something stopped, something
// running, and work that landed while nobody was looking — enough for every
// panel with a row in it to be on the grid at once, which is where the question
// this file answers is asked hardest.
func sectionLab(t *testing.T) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	mine := lab.session("-alpha", "aaaa000000000001", "the newest chat", alpha, now)
	lab.session("-alpha", "aaaa000000000002", "the older chat", alpha, now.Add(-time.Hour))
	lab.session("-beta", "bbbb000000000001", "pricing research", beta, now.Add(-2*time.Hour))
	lab.asking("-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)
	lab.session("-beta", "bbbb000000000002", "the port", beta, now.Add(-3*time.Hour))
	lab.presence("-beta", "bbbb000000000002", session.PresenceWorking, "", now)
	lab.task("-alpha", session.TaskIndexEntry{ID: "t9", SessionID: "aaaa000000000001",
		Title: "toy-scale validation", Label: "toy-scale validation",
		Status: string(session.TaskDone), EndedAt: now.Add(-time.Minute), FilesChanged: 1})

	a := lab.app(mine)
	a.width, a.height = 200, 34
	a.openHome()
	a.home.seen = now.Add(-30 * time.Minute)
	a.home.build()
	if !strings.Contains(homeText(a), "since you left") {
		t.Fatalf("nothing landed while nobody was looking, so the ledger proves nothing:\n%s", homeText(a))
	}
	return a
}

// A CURSOR IN A PANEL MARKS THAT PANEL'S HEADING AND NOTHING ELSE, whatever
// kind of panel it is — and the one the cursor is actually in, not the first
// on the grid.
func TestACursorMarksTheHeadingOfThePanelItIsIn(t *testing.T) {
	a := sectionLab(t)
	for _, panel := range []homePanelID{panelSessions, panelLeft} {
		standInPanel(t, a, panel)
		want := homeSlotOf(panel).word
		if got := markedHeadings(a); len(got) != 1 || !strings.HasPrefix(got[0], want) {
			t.Fatalf("standing in %q marks %v, want just it:\n%s", want, got, homeText(a))
		}
	}
}

// WITH THE CURSOR ON NO LINE NOTHING IS MARKED. The marked heading answers
// "where is my keyboard on this list"; a list the keyboard is not on marks
// nothing, and a screen that marked one anyway would be answering a question
// nobody had asked.
//
// THE STATE THAT ASKED THIS IS RETIRED AND THE LAW IS NOT. Home used to have a
// resting cursor — `↑` off the top row put it on no row at all — and this was
// pinned about that. `↑` reaches the TAB BAR now (pages.go's [barCursor]), so
// the only way home's own cursor is on no line is a list with no line to be on;
// the marking rule is the same either way and is asserted the same way.
func TestAHomeTheKeyboardIsNotOnMarksNoHeading(t *testing.T) {
	a := sectionLab(t)
	a.home.cursor = homeNoLine
	if got := markedHeadings(a); len(got) != 0 {
		t.Fatalf("a home with the cursor on no line marks %v, want nothing:\n%s", got, homeText(a))
	}
}

// AND NOTHING IS MARKED UNDER A SEARCH. The column is then a drop-up of matches
// and its headings are a filter's grouping rather than a place a person is
// standing in.
func TestASearchingHomeMarksNoHeading(t *testing.T) {
	a := sectionLab(t)
	typeHome(a, "pric")
	if !a.home.searching() {
		t.Fatal("typing into the box did not put home into a search")
	}
	if got := markedHeadings(a); len(got) != 0 {
		t.Fatalf("a search marks %v, want nothing:\n%s", got, homeText(a))
	}
}

// Mouse selection moves the marked heading to the same panel as its row.
func TestMouseSelectionMovesTheMarkedHeading(t *testing.T) {
	a := sectionLab(t)
	hovered := standInPanel(t, a, panelLeft)
	want := strings.Join(markedHeadings(a), "|")
	standInPanel(t, a, panelSessions)
	a.selectPlaceRow(&a.home.cursor, hovered)
	if got := strings.Join(markedHeadings(a), "|"); got != want {
		t.Fatalf("mouse selection marked %q, want %q", got, want)
	}
}

// EXACTLY ONE HEADING PER FRAME, wherever the cursor is put down. This walks
// every stop on the column rather than sampling three, because the failure it
// guards against is a section nobody thought of getting two headings or none.
func TestEveryCursorStopMarksExactlyOneHeadingOrNone(t *testing.T) {
	a := sectionLab(t)
	for at := range a.home.lines {
		if !a.home.lines[at].stop() {
			continue
		}
		a.home.cursor = at
		if got := markedHeadings(a); len(got) > 1 {
			t.Fatalf("the cursor on line %d marks %v, want at most one:\n%s", at, got, homeText(a))
		}
	}
}

// THE HEADING WEARS THE CURSOR STEP'S GROUND AND ITS WORDS DO NOT MOVE. The
// ground alone carries the fact; the words stay muted, never lit — THE ACCENT
// BUDGET forbids it (docs/DESIGN-LANGUAGE.md).
func TestTheMarkedHeadingWearsTheGroundAndKeepsItsWords(t *testing.T) {
	a := sectionLab(t)
	standInPanel(t, a, panelSessions)
	head := homeNoLine
	for at := range a.home.lines {
		if a.home.marksPanel(at) {
			head = at
		}
	}
	if head == homeNoLine {
		t.Fatalf("standing in threads marks no heading:\n%s", homeText(a))
	}
	cell := a.home.lines[head].cell
	marked := homeCellHead(cell, 40, a.pal, true, false)
	if !strings.HasPrefix(marked, cursorGround(a.pal)) {
		t.Fatalf("the marked heading wears no ground: %q", marked)
	}
	// (A heading that opens nothing is dim rather than muted, marked or not —
	// `threads` is one — and the ground still lands on it.)
	want := a.pal.muted(cell.title)
	if !homeHeadOpens(cell.panel) {
		want = a.pal.dim(cell.title)
	}
	if !strings.Contains(marked, want) || strings.Contains(marked, a.pal.accent(cell.title)) {
		t.Fatalf("the marked heading's words changed ink: %q", marked)
	}
	if rest := homeCellHead(cell, 40, a.pal, false, false); strings.HasPrefix(rest, cursorGround(a.pal)) {
		t.Fatalf("an unmarked heading wears the ground: %q", rest)
	}
}
