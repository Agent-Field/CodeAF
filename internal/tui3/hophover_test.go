package tui3

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Pointer feedback must not change the keyboard's destination or the draft.
func TestChatSwitcherHoverPaintsTheClickedRowWithoutNavigating(t *testing.T) {
	for _, mode := range []struct {
		name  string
		width int
		color tokens.Profile
	}{
		{"color", 80, tokens.ANSI256}, {"plain narrow", 40, tokens.NoColor},
	} {
		t.Run(mode.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.pal = newPalette(mode.color, false)
			a.file = "/tmp/lab/this-one.jsonl"
			keepThree(t, a)
			drive(t, a, key(hopOpenKey))
			body := make([]string, 20)
			before := a.hopOver(body, mode.width, a.pal)
			spot := a.hop.spots[1]
			selected, file := a.hop.at, a.file
			x, y := a.hop.left+2, a.hop.top+spot.row
			a.hop.live = true
			drive(t, a, motionTo(x, y))
			after := a.hopOver(body, mode.width, a.pal)
			if a.hot.kind != hoverHop || a.hot.index != spot.at || before[y] == after[y] {
				t.Fatalf("hover did not visibly reach row %d: %+v", spot.at, a.hot)
			}
			if !strings.Contains(plain(after[y]), "·") {
				t.Fatal("hover has no marker when color is absent")
			}
			if a.hop.at != selected || a.file != file || a.hop.live {
				t.Fatal("hover moved the keyboard choice or chat, or left the fade timer active")
			}
			drive(t, a, hopSettleMsg{pulse: a.hop.pulse})
			if !a.hopShowing() {
				t.Fatal("the card faded while its row was being read")
			}
			drive(t, a, motionTo(a.hop.left, y))
			if a.hot.kind != hoverNothing {
				t.Fatal("the border is an advertised target")
			}
			drive(t, a, motionTo(x, y), key("down"))
			if a.hot.kind != hoverNothing {
				t.Fatalf("keyboard navigation retained a stale pointer marker: hot=%+v open=%v at=%d", a.hot, a.hop.open, a.hop.at)
			}
			a.hopOver(body, mode.width, a.pal)
			a.hopPress(x, y)
			if a.file != "/tmp/lab/price-scrape.jsonl" {
				t.Fatal("the hovered row's click opened a different chat")
			}
		})
	}
}

func TestChatSwitcherHoverOnlyOffersItsActionableFooter(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	a.hop.rest = 2
	body := make([]string, 20)
	a.hopOver(body, 80, a.pal)
	foot := a.hop.spots[len(a.hop.spots)-1]
	if foot.at != -1 {
		t.Fatal("missing expansion target")
	}
	x, y := a.hop.left+2, a.hop.top+foot.row
	drive(t, a, motionTo(x, y))
	if a.hot.kind != hoverHop || a.hot.index != -1 {
		t.Fatal("expansion control has no hover")
	}
	a.hop.say = "This conversation is unavailable."
	a.hopOver(body, 80, a.pal)
	drive(t, a, motionTo(x, y))
	if a.hot.kind != hoverNothing {
		t.Fatal("a refusal message advertises navigation")
	}
}

// Dismissing the card consumes the press; interior furniture stays inert.
func TestChatSwitcherOutsideClickCancelsWithoutClickThrough(t *testing.T) {
	for _, edge := range []string{"left", "right", "above", "below"} {
		t.Run(edge, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.file = "/tmp/lab/this-one.jsonl"
			keepThree(t, a)
			drive(t, a, key(hopOpenKey))
			a.hop.originY = 4
			a.hopOver(make([]string, 20), 80, a.pal)
			x, y := a.hop.left+2, a.hop.top
			a.hopPress(x, y)
			if !a.hopShowing() {
				t.Fatal("card border dismissed the picker")
			}
			switch edge {
			case "left":
				x = a.hop.left - 1
			case "right":
				x = a.hop.right
			case "above":
				y = a.hop.top - 1
			case "below":
				y = a.hop.bottom
			}
			drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if a.hopShowing() || a.file != "/tmp/lab/this-one.jsonl" || a.room != nil {
				t.Fatal("outside click did not cancel in the original conversation")
			}
		})
	}
}
