package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// headerHome is where the nav drew `home` on the last frame: the head's first
// row, on every page (topnav.go).
func headerHome(t *testing.T, a *app) placeTabSpan {
	t.Helper()
	frame(a)
	for _, span := range a.tabs {
		if span.id == pageHome {
			return span
		}
	}
	t.Fatal("home is missing from the nav")
	return placeTabSpan{}
}

func TestHeaderHomePreservesBothConversationAndNewChatDrafts(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.width, a.height = 120, 40
	a.resume = func(string) (Agent, error) { t.Fatal("Home must not switch conversation"); return nil, nil }
	a.input.setText("existing chat draft")
	openStart(t, a)
	a.input.setText("new chat draft")
	home := headerHome(t, a)
	if home.from != ansi.StringWidth(" "+plain(a.pal.wordmark(a.width)))+navLead {
		t.Fatalf("home is not the nav's first word: %+v", home)
	}
	cmd, took := a.navPress(home.from, navRow)
	if !took || !a.at(pageHome) {
		t.Fatal("Home click did not open the home page")
	}
	drain(t, a, cmd)
	drive(t, a, key("esc"))
	if a.input.String() != "existing chat draft" {
		t.Fatalf("Home lost conversation draft: %q", a.input.String())
	}
	openStart(t, a)
	if a.input.String() != "new chat draft" {
		t.Fatal("Home lost the parked new-chat draft")
	}
	if lab.made != 0 || lab.agent.closes != 0 || lab.agent.stops != 0 {
		t.Fatal("navigation created or ended work")
	}
}

// THE ROWS UNDER THE STRIP ARE THE HEAD'S AND ANSWER NOTHING, and neither does
// the air on the nav's row. The rule and the blank are the seam, and the cells
// between the wordmark and `home` are furniture; a press on any of them stays
// where it landed. `home` itself answers the pointer over its pads too, and a
// plain terminal shows that with the linear mark.
func TestTheHeadAroundTheStripIsInertAndHomeHasPlainHover(t *testing.T) {
	a, _, _ := tabApp(t)
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.pal = newPalette(tokens.NoColor, false)
	home := headerHome(t, a)
	before := a.file
	for _, at := range []struct{ x, y int }{{home.from - 1, navRow}, {home.from, tabStripRow + 1}, {home.from, placeHeadRows - 1}} {
		if _, ok := a.tabAt(at.x, at.y); ok {
			t.Fatalf("cell %d,%d of the head advertises a button", at.x, at.y)
		}
		drive(t, a, tea.MouseClickMsg{X: at.x, Y: at.y, Button: tea.MouseLeft})
		if a.file != before || a.at(pageHome) {
			t.Fatalf("a press on cell %d,%d of the head navigated", at.x, at.y)
		}
	}
	for x := home.from; x < home.to; x++ {
		if !a.navHover(x, navRow) || a.tabHover != pageHome {
			t.Fatal("Home padding is not part of the target")
		}
		if !strings.Contains(plain(a.navLine(a.width, a.pal)), "·home ") {
			t.Fatal("Home has no plain-terminal hover feedback")
		}
	}
}

// THE STRIP KEEPS ITS ACTIVE TAB AND THE HEAD KEEPS ITS SHAPE AT EVERY SIZE. The
// head used to grow a row of air over the strip at thirty-two rows and another
// under it at thirty-six; it is the places' four rows now wherever the strip is
// drawn at all, and nothing below the strip's own floors.
func TestTheStripKeepsItsActiveTabAndTheHeadItsShapeAtEverySize(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	keepThree(t, a)
	a.resume = func(string) (Agent, error) { return nil, nil }
	for _, width := range []int{roomHeadFloor, 20, 24, 40, 80, 160} {
		for _, height := range []int{airyFloor, 24, 31, 32, 40, 50} {
			a.width, a.height = width, height
			a.touch()
			line := a.tabsRow(width)
			if ansi.StringWidth(line) > width {
				t.Fatalf("header overflow at %dx%d", width, height)
			}
			active := 0
			for _, hit := range a.chatTabHits {
				if hit.kind == tabHere {
					active++
				}
			}
			if active != 1 {
				t.Fatalf("lost active tab at %dx%d: %q", width, height, plain(line))
			}
			if a.tabsHeight(width) != tabStripRow+1 || a.headHeight() != chatHeadRows {
				t.Fatalf("at %dx%d the head is %d rows with the strip on row %d; it is %d, strip on %d",
					width, height, a.headHeight(), a.tabsHeight(width)-1, chatHeadRows, tabStripRow)
			}
		}
	}
	for _, size := range []struct{ w, h int }{{80, airyFloor - 1}, {roomHeadFloor - 1, 40}} {
		a.width, a.height = size.w, size.h
		a.touch()
		if a.headHeight() != 0 {
			t.Fatalf("under the strip's floors at %dx%d the head still costs %d rows", size.w, size.h, a.headHeight())
		}
	}
}

// Every navigation target has feedback even when foreground styles are disabled.
func TestPlainHeaderHoverChangesEveryActionWithoutMovingItsTarget(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.width, a.height = 160, 40
	keepThree(t, a)
	a.pal = newPalette(tokens.NoColor, false)
	rest := a.tabsRow(a.width)
	hits := append([]tabHit(nil), a.chatTabHits...)
	for _, hit := range hits {
		hot, ok := a.tabHoverAt(hit.span.from, tabStripRow)
		if !ok {
			continue
		}
		a.hot = hot
		hovered := a.tabsRow(a.width)
		if hovered == rest {
			t.Fatalf("target %v has no plain hover", hit.kind)
		}
		after, ok := a.tabAt(hit.span.from, tabStripRow)
		if !ok || after.span != hit.span || after.kind != hit.kind {
			t.Fatal("hover moved its target")
		}
		a.hot = hoverAt{}
		_ = a.tabsRow(a.width)
	}
}

// A TALLER TERMINAL IS ALL READING. The head is a constant in the conversation
// and a room's own ladder under the strip inside a node's page, so growing the
// window by a row never gives a row to chrome that the transcript had.
func TestHeaderAirDoesNotShrinkReadingWhenTerminalGrows(t *testing.T) {
	a := headRoom(t)
	a.width = 80
	grows := func(where string, pinned bool) {
		previous := 0
		for height := airyFloor; height <= 45; height++ {
			a.height = height
			a.touch()
			if pinned && a.headHeight() != chatHeadRows {
				t.Fatalf("%s at %d rows the head is %d rows, not the chat's %d", where, height, a.headHeight(), chatHeadRows)
			}
			available := height - a.headHeight()
			if previous > available {
				t.Fatalf("%s the header took reading rows on growth to %d: %d -> %d", where, height, previous, available)
			}
			previous = available
		}
	}
	grows("in a room", false)
	drive(t, a, key("esc"))
	grows("in the conversation", true)
}

// HOME KEEPS ITS WORD AND ITS CELLS BETWEEN A CONVERSATION AND THE DASHBOARD.
// Dev's #1424 asked it of the strip's home chip; here home is the nav's first
// word on every page (topnav.go), so the same promise is asked of the nav:
// lowercase, and its target in the same columns on both, on every profile.
func TestHomeTabKeepsItsSpellingAndPositionAcrossViews(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI256, tokens.TrueColor} {
		for _, width := range []int{80, 120, 160} {
			t.Run(itoa(int(profile))+"/"+itoa(width), func(t *testing.T) {
				a := newStartLab(t).app()
				a.resume = func(string) (Agent, error) { t.Fatal("home must not resume a conversation"); return nil, nil }
				a.showPage(pageNone)
				a.width, a.height = width, 40
				a.pal = newPalette(profile, false)
				chat := headerHome(t, a)
				nav := plain(a.navLine(width, a.pal))
				if !strings.Contains(nav, " home ") || strings.Contains(nav, "Home") {
					t.Fatalf("the nav's home is misspelled: %q", nav)
				}
				cmd, took := a.navPress(chat.from, navRow)
				if !took || !a.at(pageHome) {
					t.Fatal("clicking home did not open Home")
				}
				drain(t, a, cmd)
				dash := headerHome(t, a)
				if dash.from != chat.from || dash.to != chat.to {
					t.Fatalf("home moved between views: chat target=%+v dashboard target=%+v", chat, dash)
				}
			})
		}
	}
}
