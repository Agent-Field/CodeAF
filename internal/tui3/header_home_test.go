package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func headerHomeTarget(t *testing.T, a *app) tabHit {
	t.Helper()
	_ = a.tabsRow(a.width)
	for _, hit := range a.chatTabHits {
		if hit.kind == tabHome {
			return hit
		}
	}
	t.Fatal("Home is missing from navigation")
	return tabHit{}
}

func TestHeaderHomePreservesBothConversationAndNewChatDrafts(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.width, a.height = 120, 40
	a.resume = func(string) (Agent, error) { t.Fatal("Home must not switch conversation"); return nil, nil }
	a.input.setText("existing chat draft")
	openStart(t, a)
	a.input.setText("new chat draft")
	home := headerHomeTarget(t, a)
	if home.span.from != placeBarLead {
		t.Fatal("Home is not first in navigation")
	}
	cmd, took := a.tabPress(home.span.from, placeTabRow)
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

// THE ROWS AROUND THE STRIP ARE THE HEAD'S AND ANSWER NOTHING. The pulse above
// it is a reading, and the rule and the blank under it are the seam; a press on
// any of the three stays where it landed rather than opening a tab.
func TestTheHeadAroundTheStripIsInertAndHomeHasPlainHover(t *testing.T) {
	a, _, _ := tabApp(t)
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.pal = newPalette(tokens.NoColor, false)
	home := headerHomeTarget(t, a)
	before := a.file
	for _, y := range []int{0, placeTabRow + 1, placeHeadRows - 1} {
		if _, ok := a.tabAt(home.span.from, y); ok {
			t.Fatalf("row %d of the head advertises a button", y)
		}
		drive(t, a, tea.MouseClickMsg{X: home.span.from, Y: y, Button: tea.MouseLeft})
		if a.file != before || a.at(pageHome) {
			t.Fatalf("a press on row %d of the head navigated", y)
		}
	}
	for x := home.span.from; x < home.span.to; x++ {
		hot, ok := a.tabHoverAt(x, placeTabRow)
		if !ok {
			t.Fatal("Home padding is not part of the target")
		}
		a.hot = hot
		if !strings.Contains(plain(a.tabsRow(a.width)), "·home ") {
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
			if a.tabsHeight(width) != placeTabRow+1 || a.headHeight() != placeHeadRows {
				t.Fatalf("at %dx%d the head is %d rows with the strip on row %d; it is %d, strip on %d",
					width, height, a.headHeight(), a.tabsHeight(width)-1, placeHeadRows, placeTabRow)
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
		hot, ok := a.tabHoverAt(hit.span.from, placeTabRow)
		if !ok {
			continue
		}
		a.hot = hot
		hovered := a.tabsRow(a.width)
		if hovered == rest {
			t.Fatalf("target %v has no plain hover", hit.kind)
		}
		after, ok := a.tabAt(hit.span.from, placeTabRow)
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
			if pinned && a.headHeight() != placeHeadRows {
				t.Fatalf("%s at %d rows the head is %d rows, not the places' %d", where, height, a.headHeight(), placeHeadRows)
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

// The label and its padded mouse target stay in the same cells when Home
// replaces the conversation strip, including terminals with plain hover marks.
func TestHomeTabKeepsItsSpellingAndPositionAcrossViews(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI256, tokens.TrueColor} {
		for _, width := range []int{24, 40, 80, 160} {
			t.Run(itoa(int(profile))+"/"+itoa(width), func(t *testing.T) {
				a := newStartLab(t).app()
				a.resume = func(string) (Agent, error) { t.Fatal("home must not resume a conversation"); return nil, nil }
				a.showPage(pageNone)
				a.width, a.height = width, 40
				a.pal = newPalette(profile, false)
				home := headerHomeTarget(t, a)
				chat := plain(a.tabsRow(width))
				column := strings.Index(chat, "home")
				if column != placeBarLead+len(tabPad) || strings.Contains(chat, "Home") {
					t.Fatalf("conversation home label is misplaced or capitalized: %q", chat)
				}
				hot, ok := a.tabHoverAt(home.span.from, placeTabRow)
				if !ok {
					t.Fatal("home has no hover target")
				}
				a.hot = hot
				hovered := plain(a.tabsRow(width))
				at := strings.Index(hovered, "home")
				if at < 0 || ansi.StringWidth(hovered[:at]) != column || strings.Contains(hovered, "Home") {
					t.Fatalf("hover changed home's word or position: %q", hovered)
				}
				cmd, took := a.tabPress(home.span.from, placeTabRow)
				if !took || !a.at(pageHome) {
					t.Fatal("clicking home did not open Home")
				}
				drain(t, a, cmd)
				bar := plain(a.placeTabBar(width, false, a.pal))
				span := barWordSpan(t, a, pageHome)
				if strings.Index(bar, "home") != column || span.from != home.span.from || span.to != home.span.to {
					t.Fatalf("home moved between views: chat=%q dashboard=%q chat target=%+v dashboard target=%+v", chat, bar, home.span, span)
				}
			})
		}
	}
}
