package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// wallDoorApp is the strip's three-conversation window with the new-chat `+`
// on, so the door stands where it does for a person who can start one.
func wallDoorApp(t *testing.T, width int) *app {
	t.Helper()
	a, _, _ := tabApp(t)
	a.start = func(string) (Conversation, error) { return Conversation{}, nil }
	a.width = width
	a.chatTabBar = tabBar{}
	a.touch()
	_ = a.tabsRow(width)
	return a
}

// THE STRIP HAS ITS OWN DOOR TO THE CONVERSATIONS VIEW: ` ▦ All ` right after
// the new-chat `+`, touching it, its hit on its own words, and kept out of the
// tabs' own list so a walk of the tabs never meets it.
func TestTabWallDoorStandsAfterTheNewChat(t *testing.T) {
	a := wallDoorApp(t, 120)
	row := a.tabsRow(120)
	door := a.wall.door
	if !door.pressable() {
		t.Fatalf("no door on a 120-column strip: %q", plain(row))
	}
	if got := plain(ansi.Cut(row, door.from, door.to)); got != " ▦ All " {
		t.Fatalf("the door's cells say %q", got)
	}
	var plus tabHit
	for _, hit := range a.chatTabHits {
		if hit.kind == tabWall {
			t.Fatalf("the door is in the tabs' own list: %+v", hit)
		}
		if hit.kind == tabNew {
			plus = hit
		}
	}
	// ONE GAP AFTER THE `+`, the gap between any two pieces of the strip.
	if !plus.span.pressable() || plus.span.to+1 != door.from || plain(ansi.Cut(row, plus.span.to, door.from)) != " " {
		t.Fatalf("the door does not stand one gap after the +: + %+v door %+v\n%q", plus.span, door, plain(row))
	}
	// The same blank either side as the +.
	pw := plain(ansi.Cut(row, plus.span.from, plus.span.to))
	if !strings.HasPrefix(pw, " ") || !strings.HasSuffix(pw, " ") || len(pw) != 3 {
		t.Fatalf("the + is %q", pw)
	}
	if hit, ok := a.tabAt(door.from, tabStripRow); !ok || hit.kind != tabWall {
		t.Fatalf("the strip does not answer for the door: %+v", hit)
	}
	for _, w := range tabWords(a) {
		if strings.Contains(w, "All") {
			t.Fatalf("a walk of the tabs met the door: %q", w)
		}
	}
}

// UNDER THE POINTER THE DOOR WEARS THE STRIP'S HOVER GROUND, moves nothing
// else, and the hint slot names it and its key.
func TestTabWallDoorHover(t *testing.T) {
	a := wallDoorApp(t, 120)
	rest := a.tabsRow(120)
	door := a.wall.door
	hover, ok := a.tabHoverAt(door.from+2, tabStripRow)
	if !ok {
		t.Fatal("the door does not take the pointer")
	}
	a.hot = hover
	lit := a.tabsRow(120)
	if plain(lit) != plain(rest) {
		t.Fatalf("the hover moved the strip:\n%q\n%q", plain(rest), plain(lit))
	}
	cell := ansi.Cut(lit, door.from, door.to)
	if !strings.Contains(cell, "\x1b[48;") || cell == ansi.Cut(rest, door.from, door.to) {
		t.Fatalf("the hovered door wears no ground: %q", cell)
	}
	if got := a.dockHoverWords(); got != dockWallWord || !strings.Contains(got, "teams") || !strings.HasSuffix(got, wallOpenKey) {
		t.Fatalf("the hint slot says %q", got)
	}
	t.Logf("120 columns at rest:\n%q\nhovered:\n%q", plain(rest), plain(lit))
}

// A PRESS ON THE DOOR OPENS THE VIEW, the door is lit while it is up, and a
// press on it again, through the wall's own head, closes it.
func TestTabWallDoorOpensAndCloses(t *testing.T) {
	a := wallDoorApp(t, 120)
	rest := a.tabsRow(120)
	door := a.wall.door
	clickTab(t, a, door.from+1)
	if !a.wall.on {
		t.Fatal("the door did not open the view")
	}
	_ = a.wallFrame(a.width, a.height)
	open := a.tabsRow(120)
	if ansi.Cut(open, door.from, door.to) == ansi.Cut(rest, door.from, door.to) {
		t.Fatal("the door is not lit while the view is up")
	}
	t.Logf("120 columns with the view up:\n%q", plain(open))
	if _, took := a.wallPress(door.from+1, tabStripRow); !took || a.wall.on {
		t.Fatalf("a press on the door from inside the view: took %v on %v", took, a.wall.on)
	}
	// And the tab strip's own press closes it too.
	_ = a.openWall()
	clickTab(t, a, door.from+1)
	if a.wall.on {
		t.Fatal("the strip's press on the door did not close the view")
	}
}

// THE DOOR GOES WITH THE ROW, WORD AND GLYPH TOGETHER: ` ▦ All ` wherever it
// is drawn and nothing under [tabWallFrom], never a lone glyph; the count of
// hidden tabs stays the last thing on it. The row is exactly the frame's width.
func TestTabWallDoorAtEveryWidth(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 160} {
		a := wallDoorApp(t, width)
		row := a.tabsRow(width)
		if got := ansi.StringWidth(row); got != width {
			t.Fatalf("at %d columns the strip is %d cells:\n%q", width, got, plain(row))
		}
		door := a.wall.door
		want := ""
		switch {
		case width >= tabWallWordFrom:
			want = " ▦ All "
		case width >= tabWallFrom:
			want = " ▦ "
		}
		got := ""
		if door.pressable() {
			got = plain(ansi.Cut(row, door.from, door.to))
		}
		if got != want {
			t.Fatalf("at %d columns the door is %q, want %q:\n%q", width, got, want, plain(row))
		}
		for _, hit := range a.chatTabHits {
			if hit.kind != tabFold {
				continue
			}
			if door.pressable() && door.to+tabsMoreGap > hit.span.from {
				t.Fatalf("at %d columns the count is not after the door by %d: %+v %+v\n%q", width, tabsMoreGap, door, hit.span, plain(row))
			}
			for _, other := range a.chatTabHits {
				if other.span.from > hit.span.from {
					t.Fatalf("at %d columns %+v is right of the count:\n%q", width, other, plain(row))
				}
			}
		}
	}
}

// WHILE A TEAM NARROWS THE STRIP THE DOOR'S GLYPH WEARS ITS COLOUR, as the
// dock's does.
func TestTabWallDoorTakesTheTeamColour(t *testing.T) {
	a := wallDoorApp(t, 160)
	before := a.tabsRow(160)
	tabs := a.tabList()
	i, err := a.teamMake("harbor", tabs)
	if err != nil {
		t.Fatal(err)
	}
	a.wall.activeID = i
	a.touch()
	row := a.tabsRow(160)
	door := a.wall.door
	if !door.pressable() {
		t.Fatalf("no door with a team shown: %q", plain(row))
	}
	made, _ := a.teamByID(i)
	ink := a.pal.teamInk(made.HueSpec())
	if ink == nil {
		t.Skip("the palette draws no team colour")
	}
	if cell := ansi.Cut(row, door.from, door.to); !strings.Contains(cell, ink("▦")) {
		t.Fatalf("the door's glyph is not in the team's colour: %q (before %q)", cell, ansi.Cut(before, a.wall.door.from, a.wall.door.to))
	}
}
