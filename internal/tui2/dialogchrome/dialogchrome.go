// Package dialogchrome paints the floating dialog's boundary — the one thing
// 12.11 designed, drew, and then left explicitly owed.
//
// 12.11.1 settles what a dialog's boundary IS: not a border. 5.21's anti-catalog
// refuses "nested box-drawing frames", 5.13 spends the structure budget on
// "cards separated by whitespace not boxes; hairline rules only at room
// boundaries", and 7.1 refuses another harness's box style outright — so a
// floating dialog gets exactly the two marks the doc allows, a one-cell margin
// of its own ground and a hairline rule along the top and bottom of it. That
// geometry shipped as a real slot ([tui2.LayerDialogChrome]) and the shell draws
// it when nobody claims it.
//
// What it does not do is paint, and 12.11 says so in the same breath: "Owed:
// the chrome draws unpainted. The root tui2 package cannot import tokens
// without inverting the tokens → tui2 seam metrics.go documents... A lane that
// wants it tinted binds a pane to LayerDialogChrome like any other layer." This
// package is that pane, and it is a package rather than a method on one surface
// because the ring belongs to the DIALOG, not to what is inside it — the
// palette, the `?` sheet, the settings sheet and the consent dialog all float
// through the same slot and must not each grow their own answer.
//
// Two things go wrong while it is unpainted, and both were visible in a
// screenshot before they were named:
//
//   - The rule is drawn at the terminal's DEFAULT foreground, which on a dark
//     theme is the brightest cell available. So the loudest thing on a help
//     sheet is its own frame — chrome outranking every verb it surrounds, which
//     inverts 5.13's hierarchy and 8.1.6's "dim = chrome" in one stroke.
//   - The margin is a ring of the TRANSCRIPT's ground, not the dialog's. 12.11's
//     own words are "a one-cell margin of ITS OWN ground"; a margin made of the
//     room behind it is a gap, and a dialog separated from the lens by a gap
//     reads as a hole in the lens rather than as a plane above it.
//
// Both are one decision: the ring is [tokens.Sheet], the rule is
// [tokens.TextTertiary], and the ladder ground → sheet → band does the rest.
package dialogchrome

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Pane is the boundary, as a [tui2.Pane]. It holds no state a reader can move:
// it never takes focus, it answers no key, and it absorbs the clicks that land
// on it only because the shell gives a bound layer that property for free.
//
// It does not implement PaneMouse on purpose. A click on the margin must do
// NOTHING rather than something arbitrary, and a layer with no mouse handler is
// exactly that — the modal discipline covers the ring without the ring having
// an opinion about pointers.
type Pane struct {
	styler *tokens.Styler
	buf    strings.Builder
}

// New builds the chrome for a terminal profile. A nil Styler renders plain
// text, which is the posture every sibling in this tree takes and what the
// golden harness wants.
func New(s *tokens.Styler) *Pane { return &Pane{styler: s} }

// SetStyler rebinds the painter when the profile changes.
func (p *Pane) SetStyler(s *tokens.Styler) { p.styler = s }

// Render implements [tui2.Pane]: a hairline along the top row, a hairline along
// the bottom row, and the dialog's own ground between them.
//
// It draws the WHOLE rectangle rather than a ring with a hole in it, because
// the panel is composited on the plane above and covers the middle. Stating
// every cell is the cheaper correctness (12.13's ghost: a region that declines
// to state its cells shows whatever was there last), and it means a dialog one
// line taller than its content still stands on its own floor.
//
// No corners, no side rules, no title bar. A rule top and bottom is two
// hairlines at a room boundary; add the two verticals and it is the frame
// 5.21's anti-catalog refuses.
func (p *Pane) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	rule := p.paint(strings.Repeat(tokens.GlyphTreeDash, w), tokens.TextTertiary)
	if h == 1 {
		return rule
	}
	blank := p.paint(strings.Repeat(" ", w), tokens.TextTertiary)

	p.buf.Reset()
	p.buf.Grow((len(rule) + 1) * h)
	p.buf.WriteString(rule)
	for row := 1; row < h-1; row++ {
		p.buf.WriteByte('\n')
		p.buf.WriteString(blank)
	}
	p.buf.WriteByte('\n')
	p.buf.WriteString(rule)
	return p.buf.String()
}

// paint puts one run on the sheet's ground, or leaves it bare where the profile
// has no ground to give ([tokens.Profile.SheetGround]) — at 16 colours and
// below the boundary is the hairline alone, which is the degradation 12.11
// already shipped and this package only improves on where it can.
func (p *Pane) paint(text string, fg tokens.Token) string {
	if p.styler == nil {
		return text
	}
	if !p.styler.Profile().SheetGround() {
		return p.styler.PaintToken(text, fg)
	}
	return p.styler.PaintOn(text, fg, tokens.Sheet)
}
