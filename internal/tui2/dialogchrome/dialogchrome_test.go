package dialogchrome

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// -- dismissal (§16 OVERLAY DISMISSAL) ---------------------------------------

// dismissable builds a rendered ring and the counter its close increments, so a
// test asserts what the reader would see happen rather than which method ran.
func dismissable(t *testing.T, w, h int) (*Pane, *int) {
	t.Helper()
	closed := 0
	p := New(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal))
	p.SetDismiss(func() tea.Cmd { closed++; return nil })
	p.Render(w, h)
	return p, &closed
}

func leftClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}
}

// TestAClickOutsideThePanelCloses is §16's third door: an overlay closes on a
// click anywhere outside the panel, and the ring is what knows where outside
// begins. Every cell of the one-cell margin is outside — the corners included,
// which is where a reader's aim actually lands when they mean "not this".
func TestAClickOutsideThePanelCloses(t *testing.T) {
	const w, h = 20, 8
	outside := []image.Point{
		{X: 0, Y: 0}, {X: w - 1, Y: 0}, {X: 0, Y: h - 1}, {X: w - 1, Y: h - 1},
		{X: 9, Y: 0}, {X: 9, Y: h - 1}, {X: 0, Y: 4}, {X: w - 1, Y: 4},
	}
	for _, at := range outside {
		p, closed := dismissable(t, w, h)
		p.Mouse(leftClick(at.X, at.Y), at)
		if *closed != 1 {
			t.Errorf("click at %v closed %d times, want 1", at, *closed)
		}
	}
}

// TestAClickInsideThePanelDoesNothing: the panel is the dialog, and a click on
// the dialog belongs to the dialog. The overlay plane takes those before this
// pane can see them, and the answer must not depend on that — a boundary that
// dismissed on any click it was handed would close the sheet the moment the
// shell forwarded one point too many.
func TestAClickInsideThePanelDoesNothing(t *testing.T) {
	const w, h = 20, 8
	for _, at := range []image.Point{{X: 1, Y: 1}, {X: 10, Y: 4}, {X: w - 2, Y: h - 2}} {
		p, closed := dismissable(t, w, h)
		p.Mouse(leftClick(at.X, at.Y), at)
		if *closed != 0 {
			t.Errorf("click at %v inside the panel closed the dialog", at)
		}
	}
}

// TestTheDismissingClickIsSwallowed: nothing reaches the surface below. The
// shell routes one click to one layer, so what this asserts is that the ring
// hands nothing onwards that could reopen the question — a dismissal that also
// opened a rail row would make "not this" mean "not this, and that instead".
func TestTheDismissingClickIsSwallowed(t *testing.T) {
	p, closed := dismissable(t, 20, 8)
	if cmd := p.Mouse(leftClick(0, 0), image.Pt(0, 0)); cmd != nil {
		t.Errorf("the ring returned a command of its own: %v", cmd)
	}
	if *closed != 1 {
		t.Errorf("the click did not close the dialog")
	}
	// A wheel notch over the ring is a reader scrolling something they cannot
	// see. It neither closes nor falls through.
	if cmd := p.Mouse(tea.MouseWheelMsg{Button: tea.MouseWheelDown}, image.Pt(0, 0)); cmd != nil {
		t.Errorf("a wheel notch on the ring produced %v", cmd)
	}
	if *closed != 1 {
		t.Errorf("a wheel notch closed the dialog")
	}
}

// TestOnlyTheLeftButtonDismisses: right and middle are not verbs this surface
// has, and a dialog that vanished on a stray middle-click would be the ring
// inventing an action nobody asked for.
func TestOnlyTheLeftButtonDismisses(t *testing.T) {
	for _, button := range []tea.MouseButton{tea.MouseRight, tea.MouseMiddle} {
		p, closed := dismissable(t, 20, 8)
		p.Mouse(tea.MouseClickMsg{Button: button}, image.Pt(0, 0))
		if *closed != 0 {
			t.Errorf("%v closed the dialog", button)
		}
	}
}

// TestTheCloseIsTheOneTheEscPathTakes is esc parity, asserted by identity
// rather than by comparing two behaviours: the ring performs exactly the
// function it was handed and composes nothing around it, so whatever the
// overlay's own close does — flushing a debounced settings write, putting the
// keyboard back — happens the same way through both doors.
func TestTheCloseIsTheOneTheEscPathTakes(t *testing.T) {
	type escMsg struct{ n int }
	want := tea.Cmd(func() tea.Msg { return escMsg{n: 7} })
	p := New(nil)
	p.SetDismiss(func() tea.Cmd { return want })
	p.Render(20, 8)
	got := p.Mouse(leftClick(0, 0), image.Pt(0, 0))
	if got == nil {
		t.Fatal("the ring dropped the overlay's close")
	}
	// Comparing funcs is not allowed; comparing what they answer is.
	if got() != (tea.Msg(escMsg{n: 7})) {
		t.Errorf("the ring returned a command the overlay did not give it: %v", got())
	}
}

// TestAnUnboundRingIsInert is the posture this package shipped with: a chrome
// nobody wired absorbs its clicks and does nothing, so a lane that binds the
// ring and forgets [Pane.SetDismiss] is no worse off than before.
func TestAnUnboundRingIsInert(t *testing.T) {
	p := New(nil)
	p.Render(20, 8)
	if cmd := p.Mouse(leftClick(0, 0), image.Pt(0, 0)); cmd != nil {
		t.Errorf("an unbound ring answered a click with %v", cmd)
	}
}

// TestAnUnrenderedRingHasNoPanelToProtect: a click that arrives before the
// first frame — or after the layout dropped the slot — has no panel to be
// inside of, and the honest answer to "not this" is still to close.
func TestAnUnrenderedRingHasNoPanelToProtect(t *testing.T) {
	closed := 0
	p := New(nil)
	p.SetDismiss(func() tea.Cmd { closed++; return nil })
	p.Mouse(leftClick(4, 4), image.Pt(4, 4))
	if closed != 1 {
		t.Errorf("an unrendered ring closed %d times, want 1", closed)
	}
}

// TestForwardedPointsFromOffTheRingAreOutside: [Pane.Mouse] answers about
// GEOMETRY and not about which layer routed the event, which is what lets the
// shell close the REQUESTED SEAM in the package doc by forwarding a click from
// anywhere on the frame. A point above or left of the ring is negative in
// pane-local coordinates and is still, plainly, not the panel.
func TestForwardedPointsFromOffTheRingAreOutside(t *testing.T) {
	const w, h = 20, 8
	for _, at := range []image.Point{{X: -6, Y: -3}, {X: -1, Y: 4}, {X: w + 9, Y: h + 2}} {
		p, closed := dismissable(t, w, h)
		p.Mouse(leftClick(at.X, at.Y), at)
		if *closed != 1 {
			t.Errorf("forwarded point %v closed %d times, want 1", at, *closed)
		}
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
