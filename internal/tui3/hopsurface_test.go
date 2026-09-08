package tui3

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func TestSwitcherSurfaceKeepsItsFrameAndSelectedTargetAtEverySize(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	a.hop.at = len(a.hop.rows) - 1
	a.hop.rows[a.hop.at].title = "Investigate a long conversation title and its distinguishing suffix"
	a.hop.rest = 2
	for _, width := range []int{12, 24, 40, 80, 160} {
		for height := 5; height <= 20; height++ {
			lines := a.hopCardLines(width, height, a.pal)
			if len(lines) > height || len(lines) == 0 {
				t.Fatalf("%dx%d: card has %d rows", width, height, len(lines))
			}
			for at, line := range lines {
				if ansi.StringWidth(line) != width {
					t.Fatalf("%dx%d row %d escaped the frame: %q", width, height, at, plain(line))
				}
			}
			if !strings.HasPrefix(plain(lines[0]), "╭") || !strings.HasPrefix(plain(lines[len(lines)-1]), "╰") {
				t.Fatalf("%dx%d: card lost its outline", width, height)
			}
			found := false
			for _, spot := range a.hop.spots {
				if spot.at == a.hop.at {
					found = true
					if !strings.HasPrefix(plain(lines[spot.row]), "│>") {
						t.Fatalf("%dx%d: selected target has no visible position", width, height)
					}
				}
			}
			if !found {
				t.Fatalf("%dx%d: selected row is unreachable", width, height)
			}
		}
	}
}

func TestSwitcherSurfaceHasInsetAirAndSeparateSelectionAndHover(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256, tokens.ANSI16, tokens.NoColor} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.pal = newPalette(profile, false)
		a.file = "/tmp/lab/this-one.jsonl"
		keepThree(t, a)
		drive(t, a, key(hopOpenKey))
		a.hop.at = 0
		a.hot = hoverAt{kind: hoverHop, index: 1}
		lines := a.hopCardLines(80, 20, a.pal)
		if strings.Trim(plain(lines[1]), " │") != "" || strings.Trim(plain(lines[len(lines)-2]), " │") != "" {
			t.Fatal("roomy card crowds its top or bottom outline")
		}
		selected, hovered := lines[a.hop.spots[0].row], lines[a.hop.spots[1].row]
		if !strings.HasPrefix(plain(selected), "│>  1") || !strings.HasPrefix(plain(hovered), "│ · 2") {
			t.Fatal("hover is indistinguishable from the keyboard destination")
		}
		if profile == tokens.NoColor {
			if strings.Contains(strings.Join(lines, ""), "\x1b") {
				t.Fatal("plain switcher emitted terminal styling")
			}
		} else if profile == tokens.TrueColor || profile == tokens.ANSI256 {
			for _, line := range lines {
				if litCells(line) != 80 {
					t.Fatal("the modal surface has a transparent gap")
				}
			}
		}
	}
}

func TestSwitcherSurfaceUsesTheAsciiFloor(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/lab/this-one.jsonl"
	keepThree(t, a)
	drive(t, a, key(hopOpenKey))
	a.pal = newPalette(tokens.NoColor, true)
	a.hot = hoverAt{kind: hoverHop, index: 0}
	lines := a.hopCardLines(40, 16, a.pal)
	if !strings.HasPrefix(lines[0], "+---") || !strings.HasPrefix(lines[a.hop.spots[0].row], "|>. 1") {
		t.Fatal("ASCII outline, selection or hover marker was lost")
	}
}

func TestSwitcherSurfaceAndItsRowsStayDistinctAtTerminalExtremes(t *testing.T) {
	for _, base := range []measuredGround{{0, 0, 0}, {255, 255, 255}, {8, 8, 8}, {238, 238, 238}, {38, 40, 51}} {
		for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256} {
			for _, pinnedLight := range []bool{false, true} {
				pal := newPalette(profile, false)
				pal.measured, pal.ground, pal.light = true, base, pinnedLight
				pal.ramp = adaptRampFrom(pal.ramp, base)
				before := pal
				local, panel := pal.hopSurfacePalette()
				painted := func(h hue) float64 {
					if profile == tokens.ANSI256 {
						v := uint8(8 + 10*int(h.idx-232))
						return luminanceOf(v, v, v)
					}
					return luminanceOf(h.r, h.g, h.b)
				}
				ground, hover, selected := painted(panel), painted(local.ramp.cursor), painted(local.ramp.selected)
				for _, pair := range [][2]float64{{ground, luminanceOf(base.r, base.g, base.b)}, {hover, ground}, {selected, hover}} {
					if contrastRatio(pair[0], pair[1]) < 1.05 {
						t.Fatalf("base=%+v profile=%v: panel, hover or selection merged (ratio %.3f)", base, profile, contrastRatio(pair[0], pair[1]))
					}
				}
				if !reflect.DeepEqual(pal, before) {
					t.Fatal("opening the modal repainted the conversation's palette")
				}
				if base == (measuredGround{38, 40, 51}) && (local.ramp.cursor != before.ramp.cursor || local.ramp.selected != before.ramp.selected) {
					t.Fatal("a well-separated reference theme changed its row backgrounds")
				}
			}
		}
	}
}
