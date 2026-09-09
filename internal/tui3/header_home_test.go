package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
	if home.span.from != headLabelAt {
		t.Fatal("Home is not first in navigation")
	}
	cmd, took := a.tabPress(home.span.from, a.tabsLineRow())
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

func TestHeaderPaddingIsInertAndHomeHasPlainHover(t *testing.T) {
	a, _, _ := tabApp(t)
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.pal = newPalette(tokens.NoColor, false)
	home := headerHomeTarget(t, a)
	before := a.file
	for _, y := range []int{0, 2} {
		if _, ok := a.tabAt(home.span.from, y); ok {
			t.Fatal("padding advertises a button")
		}
		if cmd, took := a.tabPress(home.span.from, y); !took || cmd != nil {
			t.Fatal("padding leaked a click")
		}
	}
	if a.file != before || a.at(pageHome) {
		t.Fatal("padding navigated")
	}
	for x := home.span.from; x < home.span.to; x++ {
		hot, ok := a.tabHoverAt(x, a.tabsLineRow())
		if !ok {
			t.Fatal("Home padding is not part of the target")
		}
		a.hot = hot
		if !strings.Contains(plain(a.tabsRow(a.width)), "·Home ") {
			t.Fatal("Home has no plain-terminal hover feedback")
		}
	}
}

func TestHeaderHomeAndPaddingAdaptWithoutLosingActiveTab(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	keepThree(t, a)
	a.resume = func(string) (Agent, error) { return nil, nil }
	for _, width := range []int{12, 20, 24, 40, 80, 160} {
		for _, height := range []int{16, 31, 32, 50} {
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
			want := 1
			if width >= 48 && height >= 32 {
				want++
			}
			if width >= 48 && height >= 36 {
				want++
			}
			if a.tabsHeight(width) != want {
				t.Fatalf("wrong header budget at %dx%d", width, height)
			}
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
		hot, ok := a.tabHoverAt(hit.span.from, a.tabsLineRow())
		if !ok {
			continue
		}
		a.hot = hot
		hovered := a.tabsRow(a.width)
		if hovered == rest {
			t.Fatalf("target %v has no plain hover", hit.kind)
		}
		after, ok := a.tabAt(hit.span.from, a.tabsLineRow())
		if !ok || after.span != hit.span || after.kind != hit.kind {
			t.Fatal("hover moved its target")
		}
		a.hot = hoverAt{}
		_ = a.tabsRow(a.width)
	}
}

func TestHeaderAirDoesNotShrinkReadingWhenTerminalGrows(t *testing.T) {
	a := headRoom(t)
	a.width = 80
	previous := 0
	for height := 30; height <= 45; height++ {
		a.height = height
		a.touch()
		available := height - a.headHeight()
		if previous > available {
			t.Fatalf("header took reading rows on growth to %d: %d -> %d", height, previous, available)
		}
		previous = available
	}
}
