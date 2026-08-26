package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE ONE MARKED HEADING ──────────────────────────────────────────────────
//
// The row under the cursor says which ROW. The heading over it says which
// REGION (homesection.go holds the whole law). These tests pin the four facts
// that make it worth a rung: it follows the keyboard, it names the section that
// owns the cursor whatever kind of section that is, there is never more than one
// of it, and there is none of it at rest or under a search.
//
// THE SECTIONS ARE THE SWITCHER'S NOW. They used to be two strips and a heading
// per project; the resting column is one flat ranked list, and the lines over it
// that NAME rows rather than being one are all [homeSwitchHead] — the `since you
// left` heading, the claim over the ranked rows, and a project's name while
// `alt+g` is grouping. [headingKind] was widened to take them and [sectionEnd]
// is the blank row alone, so the walk is unchanged: the nearest heading that owns
// the cursor's row, stopping at the first block boundary above it.
//
// ── WHAT THE PAINT DOES NOT YET DO ──────────────────────────────────────────
//
// [app.homeLine] hands every line of the reading to [switcherReading.paint],
// which draws a heading dim and never asks [homeView.sectionInk] — so the ink
// step the law is about does not reach the screen on the resting list today.
// What is asserted below is therefore the WALK and the paint's own two channels
// ([homeView.sectionInk]'s answer, and that a heading takes no ground), which is
// as close to the screen as this law can honestly be tested until the switcher's
// painter asks the same question the project heading's painter does.

// cursorGround is the escape sequence THE GROUND LADDER's cursor step opens
// with, asked of the painter itself ([palette.cursor]) so this file names a STEP
// rather than a hex value the ladder is free to retune.
func cursorGround(pal palette) string {
	lead, _, _ := strings.Cut(pal.cursor(" ", 1), " ")
	return lead
}

// selectedGround is the same question of the step one rung up, which is what
// tells a heading's ground from the ground the conversation this terminal is in
// already wears.
func selectedGround(pal palette) string {
	lead, _, _ := strings.Cut(pal.selected(" ", 1), " ")
	return lead
}

// headingWord is what one heading line says, whichever kind of heading it is.
// The claim over the ranked rows carries no word of its own — it is assembled
// from the chat count at paint time — so it answers by the one clause that is
// always in it.
func headingWord(line homeLine) string {
	switch {
	case line.sw != nil && line.sw.section:
		return "what wants you first"
	case line.sw != nil:
		return line.sw.heading
	}
	return line.project
}

// markedHeadings is every heading on the built column the frame is MARKING,
// named by the word it carries — the assertion this whole file is about, and a
// slice rather than a single value so "exactly one" is a thing a test can see
// fail.
func markedHeadings(a *app) []string {
	var out []string
	for at, line := range a.home.lines {
		if !headingKind(line.kind) {
			continue
		}
		if a.home.marksSection(at) {
			out = append(out, headingWord(line))
		}
	}
	return out
}

// standInList puts the cursor on the first conversation of one project's block,
// and answers the line it landed on. It walks the built lines rather than
// counting, because how many rows a ledger and a claim put above the first block
// is not a number a test may know.
func standInList(t *testing.T, a *app, name string) int {
	t.Helper()
	for at, line := range a.home.lines {
		if line.kind == homeSession && line.sw != nil && line.sw.row != nil && line.sw.row.project == name {
			a.home.cursor = at
			return at
		}
	}
	t.Fatalf("no conversation of %q is on the list:\n%s", name, homeText(a))
	return homeRest
}

// sectionLab is a machine with two projects, something stopped, something
// running, and work that landed while nobody was looking — enough for the
// ledger, the claim and a heading per project to be on one column at once, which
// is where the question this file answers is asked hardest. It is grouped by
// `alt+g` for that last one.
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
	if !a.placeAlt('g') {
		t.Fatal("alt+g did nothing, so this column has no project headings on it")
	}
	if !strings.Contains(homeText(a), "since you left") {
		t.Fatalf("nothing landed while nobody was looking, so the ledger proves nothing:\n%s", homeText(a))
	}
	return a
}

// A CURSOR IN A BLOCK MARKS THAT BLOCK'S HEADING AND NOTHING ELSE, whatever kind
// of block it is — and it marks the one the cursor is actually in, not the first
// one on the column.
func TestACursorMarksTheHeadingOfTheSectionItIsIn(t *testing.T) {
	a := sectionLab(t)
	for _, name := range []string{"alpha", "beta"} {
		standInList(t, a, name)
		if got := markedHeadings(a); len(got) != 1 || got[0] != name {
			t.Fatalf("standing in %q marks %v, want just %q:\n%s", name, got, name, homeText(a))
		}
	}
	// AND THE LEDGER IS A SECTION LIKE ANY OTHER. Its heading names what the
	// lines under it are about — the time you were away — so a cursor on one of
	// them marks it and neither project.
	at := homeRest
	for i, line := range a.home.lines {
		if line.kind == homeLedger {
			at = i
			break
		}
	}
	if at == homeRest {
		t.Fatalf("the ledger has no lines under it:\n%s", homeText(a))
	}
	a.home.cursor = at
	got := markedHeadings(a)
	if len(got) != 1 || !strings.HasPrefix(got[0], "since you left") {
		t.Fatalf("standing on a ledger line marks %v, want the `since you left` heading:\n%s", got, homeText(a))
	}
}

// AT REST NOTHING IS MARKED. Rest is the morning glance — no row is chosen, so
// no region is either, and a screen that marked one would be answering a
// question nobody had asked yet.
func TestHomeAtRestMarksNoHeading(t *testing.T) {
	a := sectionLab(t)
	a.home.cursor = homeRest
	if !a.home.resting() {
		t.Fatal("home is not at rest with the cursor on homeRest")
	}
	if got := markedHeadings(a); len(got) != 0 {
		t.Fatalf("home at rest marks %v, want nothing:\n%s", got, homeText(a))
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

// THE POINTER MOVES NOTHING. A hover previews a card without moving the
// selection ([homeView.previewLine]), and the marked heading answers "where is
// my keyboard" — so a pointer resting in another block must leave it exactly
// where the cursor put it.
func TestAHoverDoesNotMoveTheMarkedHeading(t *testing.T) {
	a := sectionLab(t)
	standInList(t, a, "alpha")
	want := markedHeadings(a)
	if len(want) != 1 {
		t.Fatalf("the cursor marks %v to begin with, so the hover proves nothing", want)
	}

	// The pointer goes to a row in the OTHER block — a different section under a
	// different heading.
	hovered := standInList(t, a, "beta")
	a.home.cursor = homeRest
	standInList(t, a, "alpha")
	a.home.hover = hovered

	if got := markedHeadings(a); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the pointer moved the marked heading from %v to %v:\n%s", want, got, homeText(a))
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

// THE HEADING BRIGHTENS AND WEARS NO GROUND. Its word steps up from dim to the
// body ink, and no band goes under it: a ground on this screen means where a
// person's hands are, and a frame with the cursor's band on a row AND an
// identical band on the heading read as two selections — the exact confusion this
// mark exists to end (homesection.go argues it in full).
//
// THE INK STEP IS ASKED OF [homeView.sectionInk] RATHER THAN OF THE DRAWN ROW,
// because the switcher's own painter does not yet consult it (this file's header
// says so); the no-ground half is asked of the row as it is really drawn, which
// is where it can be.
func TestTheMarkedHeadingBrightensAndWearsNoGround(t *testing.T) {
	a := sectionLab(t)
	at := standInList(t, a, "alpha")
	heading := homeRest
	for i := at; i >= 0; i-- {
		if headingKind(a.home.lines[i].kind) {
			heading = i
			break
		}
	}
	if heading == homeRest || headingWord(a.home.lines[heading]) != "alpha" {
		t.Fatalf("the alpha block has no heading of its own:\n%s", homeText(a))
	}
	if got := a.home.sectionInk(heading, a.pal)("alpha"); got != a.pal.ink("alpha") {
		t.Fatalf("the marked heading's word did not step up to the body ink: %q", got)
	}
	// AND EVERY OTHER HEADING STAYS DIM, which is what makes one step up mean
	// something at all.
	for i, line := range a.home.lines {
		if i == heading || !headingKind(line.kind) {
			continue
		}
		if got := a.home.sectionInk(i, a.pal)("x"); got != a.pal.dim("x") {
			t.Fatalf("heading %q is lit while the cursor is elsewhere: %q", headingWord(line), got)
		}
	}
	// AND NOT THE ACCENT: brighter is not lit, and the budget stands.
	if sgrOf(a.pal.ink) == sgrOf(a.pal.accent) {
		t.Fatal("the body ink and the accent are the same paint, so a marked heading spends the budget")
	}
	// AND THE HEADING TAKES NEITHER GROUND ON THE FRAME. A heading is not a
	// cursor stop, so no band can reach it — and this is the assertion that says
	// so about the pixels rather than about the predicate.
	drawn := a.homeLine(a.home.lines[heading], heading, 120, a.pal)
	if strings.HasPrefix(drawn, cursorGround(a.pal)) || strings.HasPrefix(drawn, selectedGround(a.pal)) {
		t.Fatalf("the marked heading wears a ground, which is the hand's channel: %q", drawn)
	}
}
