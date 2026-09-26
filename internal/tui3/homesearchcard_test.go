package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// searchCardLab opens three missing-folder matches in one Home search.
func searchCardLab(t *testing.T, width int) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	alpha := lab.session("-alpha", "aaaa000000000001", "Seed Alpha", filepath.Join(lab.work, "missing-alpha"), now)
	lab.session("-beta", "bbbb000000000001", "Seed Beta", filepath.Join(lab.work, "missing-beta"), now.Add(-time.Minute))
	lab.session("-gamma", "cccc000000000001", "Seed Gamma", filepath.Join(lab.work, "missing-gamma"), now.Add(-2*time.Minute))
	a := lab.app(alpha)
	a.width, a.height = width, 40
	a.openHome()
	for _, letter := range "Seed" {
		a.homeKey(key(string(letter)))
	}
	a.homeFrame(a.width, a.height)
	return a
}

// searchCardOnScreen reads only the space reserved for the card, so a match in
// the list cannot make an absent card look present.
func searchCardOnScreen(a *app) string {
	left, right := homeColumns(a.width)
	if right == 0 {
		return ""
	}
	rows, _, _, _ := a.homeFrame(a.width, a.height)
	var card []string
	for _, row := range rows {
		card = append(card, ansi.Strip(ansi.Cut(row, left+homeGutter, a.width)))
	}
	return strings.Join(card, "\n")
}

func TestHomeSearchCardFollowsKeyboardCursor(t *testing.T) {
	a := searchCardLab(t, 136)
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	// A pointer may briefly preview another match; the next arrow still walks
	// from the keyboard's match and the card follows that walk.
	at := homeLineOfKind(t, a, homeSession, "missing-beta")
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: homeLineY(t, a, at)})
	drive(t, a, key("up"))
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession {
		t.Fatalf("the arrow did not pick a match: %+v", line)
	}
	if got := homeName(line.row); got != "Seed Beta" {
		t.Fatalf("the arrow walked from the pointer instead of the cursor: got %q", got)
	}
	if card := searchCardOnScreen(a); !strings.Contains(card, homeName(line.row)) {
		t.Fatalf("the cursor's match has no card on the right:\n%s", card)
	}
}

func TestHomeSearchCardFollowsPointerMatch(t *testing.T) {
	a := searchCardLab(t, 180)
	cursor := a.home.cursor
	at := homeLineOfKind(t, a, homeSession, "missing-beta")
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: homeLineY(t, a, at)})
	if card := searchCardOnScreen(a); !strings.Contains(card, "Seed Beta") {
		t.Fatalf("the pointer's match has no card on the right:\n%s", card)
	}
	if a.home.cursor != cursor {
		t.Fatalf("the pointer moved the keyboard cursor from %d to %d", cursor, a.home.cursor)
	}
	// The copy chord must act on the card the pointer brought up before the
	// keyboard takes selection back from that pointer.
	path := a.home.lines[at].row.Workspace
	drive(t, a, key("ctrl+y"))
	if got := a.home.msg; got != "copied "+path {
		t.Fatalf("the visible card is about Seed Beta but copy answered %q", got)
	}
}

func TestHomeSearchCardReturnsToCursorWhenPointerLeaves(t *testing.T) {
	a := searchCardLab(t, 180)
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession {
		t.Fatalf("two up keys did not pick a match: %+v", line)
	}
	want := homeName(line.row)
	at := homeLineOfKind(t, a, homeSession, "missing-beta")
	y := homeLineY(t, a, at)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	left, _ := homeColumns(a.width)
	drive(t, a, tea.MouseMotionMsg{X: left + homeGutter + 1, Y: y})
	if card := searchCardOnScreen(a); !strings.Contains(card, want) {
		t.Fatalf("off the list, the cursor's card did not return: want %q in\n%s", want, card)
	}
}

func TestHomeSearchHasNoSideCardBelow136Columns(t *testing.T) {
	if homeCardMin != 136 {
		t.Fatalf("the manual's card floor is 136 columns, code says %d", homeCardMin)
	}
	a := searchCardLab(t, 135)
	a.homeKey(key("up"))
	a.homeKey(key("up"))
	cursor := a.home.cursor
	at := homeLineOfKind(t, a, homeSession, "missing-beta")
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: homeLineY(t, a, at)})
	if card := searchCardOnScreen(a); card != "" {
		t.Fatalf("a narrow search drew a side card:\n%s", card)
	}
	rows, _, _, _ := a.homeFrame(a.width, a.height)
	if frame := ansi.Strip(strings.Join(rows, "\n")); strings.Count(frame, "Seed Beta") != 1 {
		t.Fatalf("at 135 columns the hovered match appears outside its one list row:\n%s", frame)
	}
	if a.home.cursor != cursor {
		t.Fatalf("at 135 columns the pointer moved the keyboard cursor from %d to %d", cursor, a.home.cursor)
	}
}
