package dialogchrome

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// rows splits a render and strips the paint, so a shape assertion is about
// cells and never about bytes (12.11's own method note: where a law is about
// what a person perceives, measure the perception).
func rows(s string) []string {
	out := strings.Split(s, "\n")
	for i := range out {
		out[i] = ansi.Strip(out[i])
	}
	return out
}

// TestShapeIsTheBoundaryAndNothingElse: a rule top and bottom, ground between,
// no corners and no verticals. Adding the two verticals is the box-drawing frame
// 5.21's anti-catalog refuses, and it would arrive here first.
func TestShapeIsTheBoundaryAndNothingElse(t *testing.T) {
	p := New(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	const w, h = 10, 4
	got := rows(p.Render(w, h))
	if len(got) != h {
		t.Fatalf("rendered %d rows, want %d", len(got), h)
	}
	rule := strings.Repeat(tokens.GlyphTreeDash, w)
	blank := strings.Repeat(" ", w)
	want := []string{rule, blank, blank, rule}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEveryCellIsStated is 12.13's ghost, one plane up: a region that returns
// fewer cells than it was given shows whatever the last frame left there.
func TestEveryCellIsStated(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor} {
		p := New(tokens.NewStyler(profile, tokens.FocusNormal))
		for w := 1; w <= 40; w++ {
			for h := 1; h <= 6; h++ {
				got := rows(p.Render(w, h))
				if len(got) != h {
					t.Fatalf("%v at %dx%d: %d rows", profile, w, h, len(got))
				}
				for i, line := range got {
					if n := blocks.Width(line); n != w {
						t.Fatalf("%v at %dx%d: row %d is %d cells, want %d",
							profile, w, h, i, n, w)
					}
				}
			}
		}
	}
}

// TestTheRingStandsOnTheDialogsOwnGround closes 12.11's owed item: the margin is
// "a one-cell margin of its own ground", and a margin made of the room behind it
// is a gap rather than a boundary.
func TestTheRingStandsOnTheDialogsOwnGround(t *testing.T) {
	p := New(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	ground := tokens.Sheet.Bg(tokens.TrueColor, tokens.FocusNormal)
	for i, line := range strings.Split(p.Render(12, 3), "\n") {
		if !strings.Contains(line, ground) {
			t.Errorf("row %d carries no sheet ground: %q", i, line)
		}
	}
}

// TestTheRuleRecedes: chrome is the dim tier (8.1.6, 5.13). Unpainted, the rule
// inherits the terminal's default foreground — the brightest cell on a dark
// theme — and the boundary outranks every verb it surrounds.
func TestTheRuleRecedes(t *testing.T) {
	p := New(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	first := strings.Split(p.Render(12, 3), "\n")[0]
	if want := tokens.TextTertiary.Fg(tokens.TrueColor, tokens.FocusNormal); !strings.Contains(first, want) {
		t.Errorf("the hairline is not drawn at the chrome tier: %q", first)
	}
	for _, loud := range []tokens.Token{tokens.TextPrimary, tokens.TextSecondary} {
		if strings.Contains(first, loud.Fg(tokens.TrueColor, tokens.FocusNormal)) {
			t.Errorf("the hairline is drawn at %s, which outranks what it surrounds", loud)
		}
	}
}

// TestNoGroundWhereTheProfileHasNone: at 16 colours the only raised background
// is bright black, which belongs to the user's theme
// ([tokens.Profile.SheetGround]) — and the reverse-video fallback PaintOn would
// otherwise take there would turn the whole ring into a slab.
func TestNoGroundWhereTheProfileHasNone(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI16} {
		out := New(tokens.NewStyler(profile, tokens.FocusNormal)).Render(12, 3)
		if strings.Contains(out, "\x1b[7m") {
			t.Errorf("%v: the ring reversed into a slab", profile)
		}
		if profile == tokens.NoColor && strings.Contains(out, "\x1b") {
			t.Errorf("no-colour ring emitted escapes: %q", out)
		}
	}
}

// TestNilStylerRendersPlain is the posture every sibling takes: a component
// built before a profile was chosen draws the shape and no colour.
func TestNilStylerRendersPlain(t *testing.T) {
	out := (&Pane{}).Render(8, 3)
	if strings.Contains(out, "\x1b") {
		t.Errorf("nil styler painted: %q", out)
	}
	if got := len(rows(out)); got != 3 {
		t.Errorf("nil styler rendered %d rows, want 3", got)
	}
}

// TestDegenerateSizes: a pane is asked for odd rectangles during a resize and
// must draw less rather than fail.
func TestDegenerateSizes(t *testing.T) {
	p := New(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	for _, size := range [][2]int{{0, 0}, {-1, 4}, {4, -1}, {0, 3}, {3, 0}} {
		if got := p.Render(size[0], size[1]); got != "" {
			t.Errorf("%dx%d rendered %q, want nothing", size[0], size[1], got)
		}
	}
	if got := rows(p.Render(6, 1)); len(got) != 1 || got[0] != strings.Repeat(tokens.GlyphTreeDash, 6) {
		t.Errorf("a one-row ring is one rule, got %q", got)
	}
}
