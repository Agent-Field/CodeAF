package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// The filled surface owns a blank before its status and after its close mark.
// Those cells must survive selection, hover, and a terminal without colors.
func TestTabInsetSurroundsPaintedStatusAndClose(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI256, tokens.TrueColor} {
		for _, signal := range []tabSignal{tabIdle, tabWorking, tabNeedsPerson} {
			for _, active := range []bool{false, true} {
				a := newTestApp(&fakeAgent{})
				a.pal = newPalette(profile, false)
				tab := chatTab{key: "inset", word: "Readable title", here: active, signal: signal}
				pieces, hits := a.tabsFit([]chatTab{tab}, 70, 0)
				a.chatTabHits = hits
				var label, close tabHit
				for _, hit := range hits {
					if hit.kind == tabClose {
						close = hit
					} else if hit.kind == tabHere || hit.kind == tabOther {
						label = hit
					}
				}
				if !label.span.pressable() || !close.span.pressable() {
					t.Fatal("fixture has no label or close")
				}
				for _, hover := range []int{-1, label.span.from, close.span.to - 1} {
					a.hot = hoverAt{}
					if hover >= 0 {
						for _, hit := range hits {
							if hit.span.holds(hover) {
								a.hot = hoverAt{kind: hoverTab, index: hit.span.from}
							}
						}
					}
					line := a.tabsPaint(pieces)
					first := ansi.Cut(line, label.span.from, label.span.from+1)
					last := ansi.Cut(line, close.span.to-1, close.span.to)
					// WITH NO COLOUR, THE POINTER'S TAB WEARS `·` IN ITS LEADING INSET,
					// as a nav word does; every other edge is a blank.
					lead := " "
					if profile == tokens.NoColor && hover >= 0 {
						lead = "·"
					}
					if plain(first) != lead || plain(last) != " " {
						t.Fatalf("profile=%v signal=%v active=%v hover=%d: tab edges are not inset: %q / %q", profile, signal, active, hover, first, last)
					}
					if profile >= tokens.ANSI256 && (active || hover >= 0) {
						if !strings.Contains(first, "\x1b[48;") || !strings.Contains(last, "\x1b[48;") {
							t.Fatalf("insets sit outside the filled surface: %q / %q", first, last)
						}
					}
					if active || hover >= 0 {
						if got := plain(ansi.Cut(line, close.span.from, close.span.to)); !strings.Contains(got, a.tabCloseWord()) {
							t.Fatalf("inset lost the visible close mark: %q", got)
						}
					}
					if signal != tabIdle && !strings.Contains(plain(line), tabSignalGlyph(signal, false)) {
						t.Fatalf("inset hid the status: %q", plain(line))
					}
				}
			}
		}
	}
}

// A padded label remains a selection target; the trailing inset belongs only
// to closing that tab, including when the tab is not the current conversation.
func TestTabInsetCellsKeepTheirNavigationAndCloseOwnership(t *testing.T) {
	for _, closeIt := range []bool{false, true} {
		a, older, newer := tabApp(t)
		before := a.file
		word := "openrouter price scrape"
		label := tabSpanFor(t, a, word)
		close := tabCloseSpanFor(t, a, word)
		x, want := label.from, tabOther
		if closeIt {
			x, want = close.to-1, tabClose
		}
		hit, ok := a.tabAt(x, tabStripRow)
		if !ok || hit.kind != want || hit.tab.word != word {
			t.Fatalf("inset maps to wrong target: %+v", hit)
		}
		cmd, took := a.tabPress(x, tabStripRow)
		if !took {
			t.Fatal("inset click fell through")
		}
		drain(t, a, cmd)
		if closeIt {
			if a.file != before || strings.Contains(plain(a.tabsRow(a.width)), word) {
				t.Fatal("close inset switched chats or left its tab visible")
			}
		} else if a.file != hit.tab.file {
			t.Fatalf("leading inset did not open its chat: %q", a.file)
		}
		if older.closes+newer.closes+older.stops+newer.stops != 0 {
			t.Fatal("tab inset navigation ended work")
		}
	}
}

// The least roomy strip still gives the current chat its two insets and a close
// target; adding air must neither overflow nor silently remove the active tab.
func TestTabInsetsFitNarrowFramesWithoutLosingTheActiveTab(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI256} {
		for _, width := range []int{12, 40} {
			a, _, _ := tabApp(t)
			a.pal = newPalette(profile, false)
			a.width = width
			a.state = stateWorking
			a.touch()
			line := a.tabsRow(width)
			if ansi.StringWidth(line) > width {
				t.Fatalf("inset overflow at %d columns: %q", width, plain(line))
			}
			active := 0
			for _, hit := range a.chatTabHits {
				if hit.kind == tabHere {
					active++
					if plain(ansi.Cut(line, hit.span.from, hit.span.from+1)) != " " {
						t.Fatalf("leading inset disappeared at %d: %q", width, plain(line))
					}
				}
				if hit.kind == tabClose && hit.tab.here {
					if plain(ansi.Cut(line, hit.span.to-1, hit.span.to)) != " " {
						t.Fatalf("close inset disappeared at %d: %q", width, plain(line))
					}
				}
				if hit.span.from < 0 || hit.span.to > width {
					t.Fatalf("inset target exceeds frame: %+v", hit)
				}
			}
			if active != 1 {
				t.Fatalf("insets lost the active tab at %d: %q", width, plain(line))
			}
		}
	}
}
