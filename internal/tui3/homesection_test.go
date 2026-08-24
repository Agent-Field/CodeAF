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
// REGION, which is the question a three-column frame asks and a single quiet
// tint could not answer (homesection.go holds the whole law). These tests pin
// the four facts that make it worth a rung: it follows the keyboard, it names
// the section that owns the cursor whatever kind of section that is, there is
// never more than one of it, and there is none of it at rest or under a search.

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
func headingWord(line homeLine) string {
	if line.kind == homeElsewhereRule {
		return homeElsewhereWord
	}
	return line.project
}

// markedHeadings is every heading on the built column that is drawn ON A
// GROUND, named by the word it carries — the assertion this whole file is
// about, and a slice rather than a single value so "exactly one" is a thing a
// test can see fail.
//
// It draws through [app.homeLine], which is the one painter both of the wide
// frame's columns go through ([app.homeRows]), so what it reads is what the
// screen draws and not a second opinion about it.
func markedHeadings(a *app) []string {
	ground := cursorGround(a.pal)
	var out []string
	for at, line := range a.home.lines {
		if !headingKind(line.kind) {
			continue
		}
		if strings.HasPrefix(a.homeLine(line, at, homeListCap, a.pal), ground) {
			out = append(out, headingWord(line))
		}
	}
	return out
}

// standInZone puts the cursor on the first row of one strip, and standInList on
// the first conversation of the list below. Both walk the built lines rather
// than counting, for [homeView.zoneSplit]'s reason.
func standInZone(t *testing.T, a *app, word string) int {
	t.Helper()
	for at, line := range a.home.lines {
		if line.zone != nil && line.zone.word == word {
			a.home.cursor = at
			return at
		}
	}
	t.Fatalf("the %q zone gathered no rows:\n%s", word, homeText(a))
	return homeRest
}

func standInList(t *testing.T, a *app, name string) int {
	t.Helper()
	for at := a.home.placesFrom(); at < len(a.home.lines); at++ {
		line := a.home.lines[at]
		if line.kind == homeSession && line.project == name {
			a.home.cursor = at
			return at
		}
	}
	t.Fatalf("no conversation of %q is on the list:\n%s", name, homeText(a))
	return homeRest
}

// sectionLab is a machine with two projects, something stopped and something
// running — enough for both strips to have rows and for the list to have two
// headings to tell apart. The frame is wide enough for [homeTierColumns], which
// is where the question this file answers is asked hardest.
func sectionLab(t *testing.T) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "the newest chat", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "the older chat", "/tmp/alpha", now.Add(-time.Hour))
	lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-2*time.Hour))
	lab.asking("-tmp-beta", "bbbb000000000001", consentQuestion(7, "needs your ok to run bash"), now)
	lab.session("-tmp-beta", "bbbb000000000002", "the port", "/tmp/beta", now.Add(-3*time.Hour))
	lab.presence("-tmp-beta", "bbbb000000000002", session.PresenceWorking, "", now)

	a := lab.app(mine)
	a.width, a.height = 140, 30
	a.openHome()
	if !a.home.columns() {
		t.Fatalf("a %d-cell frame is not the columns tier", a.width)
	}
	return a
}

// A CURSOR IN A STRIP MARKS THAT STRIP'S LABEL AND NOTHING IN THE LIST.
func TestACursorInAZoneMarksThatZonesLabelAlone(t *testing.T) {
	a := sectionLab(t)
	for _, word := range []string{attentionNeedsWord, attentionMovingWord} {
		standInZone(t, a, word)
		if got := markedHeadings(a); len(got) != 1 || got[0] != word {
			t.Fatalf("standing in %q marks %v, want just %q:\n%s", word, got, word, homeText(a))
		}
	}
}

// A CURSOR IN A PROJECT MARKS THAT PROJECT'S HEADING AND NEITHER LABEL — and it
// marks the project the cursor is actually in, not the first one on the column.
func TestACursorInAProjectMarksThatProjectsHeadingAlone(t *testing.T) {
	a := sectionLab(t)
	for _, name := range []string{"alpha", "beta"} {
		standInList(t, a, name)
		if got := markedHeadings(a); len(got) != 1 || got[0] != name {
			t.Fatalf("standing in %q marks %v, want just %q:\n%s", name, got, name, homeText(a))
		}
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
// my keyboard" — so a pointer resting in the other column must leave it exactly
// where the cursor put it.
func TestAHoverDoesNotMoveTheMarkedHeading(t *testing.T) {
	a := sectionLab(t)
	standInList(t, a, "alpha")
	want := markedHeadings(a)

	// The pointer goes to a row in the OTHER region — a zone row, which is a
	// different section under a different heading.
	hovered := homeRest
	for at, line := range a.home.lines {
		if line.zone != nil && line.stop() {
			hovered = at
			break
		}
	}
	if hovered == homeRest {
		t.Fatalf("no zone row to point at:\n%s", homeText(a))
	}
	a.home.hover = hovered

	if got := markedHeadings(a); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the pointer moved the marked heading from %v to %v:\n%s", want, got, homeText(a))
	}
}

// EXACTLY ONE HEADING PER FRAME, wherever the cursor is put down. This walks
// every stop on the column rather than sampling three, because the failure this
// guards against is a section nobody thought of getting two headings or none.
func TestEveryCursorStopMarksExactlyOneHeadingOrNone(t *testing.T) {
	a := sectionLab(t)
	// The archive is a section of ONE ROW with no heading over it, so a cursor
	// on it marks nothing — which is the honest answer and the reason the walk
	// stops at a blank rather than reaching into the block above.
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

// THE HEADING TAKES THE CURSOR STEP AND NEVER THE SELECTED ONE, because the
// selected step is already spent on this column: it is the conversation this
// terminal is holding on screen. Two meanings for one rung is what the ladder's
// refusal of a fifth step exists to prevent (homesection.go argues it in full).
func TestTheMarkedHeadingWearsTheCursorStepAndNotTheSelectedOne(t *testing.T) {
	a := sectionLab(t)
	at := standInList(t, a, "alpha")
	heading := homeRest
	for i := at; i >= 0; i-- {
		if a.home.lines[i].kind == homeHeading {
			heading = i
			break
		}
	}
	if heading == homeRest {
		t.Fatalf("the alpha block has no heading:\n%s", homeText(a))
	}
	drawn := a.homeLine(a.home.lines[heading], heading, homeListCap, a.pal)
	if !strings.HasPrefix(drawn, cursorGround(a.pal)) {
		t.Fatalf("the marked heading is not on the cursor step: %q", drawn)
	}
	if strings.HasPrefix(drawn, selectedGround(a.pal)) {
		t.Fatalf("the marked heading took the selected step, which this column already spends: %q", drawn)
	}
	// AND THE WORD DOES NOT CHANGE TIER. The accent budget forbids lighting a
	// heading, so what moved is the ground and only the ground.
	if !strings.Contains(drawn, a.pal.dim("alpha")) {
		t.Fatalf("the marked heading's word left the dim tier: %q", drawn)
	}
}
