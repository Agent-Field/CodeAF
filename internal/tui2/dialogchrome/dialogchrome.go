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
//
// The third thing this package owns is DISMISSAL, and it is here for the same
// reason the paint is: the ring is the only part of the surface that knows what
// "outside the panel" means. Design law §16 OVERLAY DISMISSAL says every overlay
// closes three ways — its `close esc` chip, the esc key, and a click anywhere
// outside the panel — and names this package as the owner of the third. See
// [Pane.Mouse].
//
// REQUESTED SEAM — how far "outside" currently reaches. The ring is a slot with
// a rectangle, and the shell routes a click to the layer whose rectangle holds
// it. So what arrives here today is every click on the one-cell margin, which
// is genuinely outside the panel and is dismissed. A click further out — on the
// transcript, on the rail — still lands on the pane it is over, because a
// floating dialog is modal by z order and not by area.
//
// Closing that gap is three lines in [tui2.Shell.mouse] and none of them belong
// in this package: while the overlay plane is up, a click that hits neither
// [tui2.LayerOverlay] nor [tui2.LayerDialogChrome] is forwarded HERE instead of
// to the pane underneath. [Pane.Mouse] is written to be that pane's answer
// already — it tests the geometry rather than the route, so a forwarded point
// from anywhere on the frame is outside the panel and dismisses, and nothing
// beneath the dialog ever sees the click.
package dialogchrome

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ring is how many cells of margin the chrome holds on every side, and it is
// the pane's OWN geometry rather than a copy of the shell's: [Pane.Render]
// rules row 0 and row h-1 and grounds one column at each end, so what is left
// inside is exactly the panel the overlay plane draws on top. Reading the
// number off the drawing rather than off [tui2] is what keeps the hit test and
// the paint from being two truths that can drift.
const ring = 1

// Pane is the boundary, as a [tui2.Pane]. It holds no state a reader can move:
// it never takes focus and it answers no key.
//
// It DOES answer the pointer, and that is the one thing 12.11 left it unable to
// do. A layer with no mouse handler absorbs its clicks and does nothing with
// them, which was the right posture while the ring was only a margin; §16 makes
// the ring a verb — the click that lands beside a dialog is a reader saying
// "not this", and every other surface in the product answers that. See
// [Pane.Mouse] and [Pane.SetDismiss].
type Pane struct {
	styler *tokens.Styler
	buf    strings.Builder

	// dismiss is the close a click outside the panel performs. It is the
	// overlay's OWN close and never a second one — see [Pane.SetDismiss].
	dismiss func() tea.Cmd

	// w and h are the rectangle the ring was last drawn at, kept so a pointer
	// can be resolved against the boundary that is actually on screen. It is
	// the one number a pane may keep, because it IS the rectangle the pane was
	// told about and nothing else can know it (the same field, for the same
	// reason, that the footer's row keeps).
	w, h int
}

// New builds the chrome for a terminal profile. A nil Styler renders plain
// text, which is the posture every sibling in this tree takes and what the
// golden harness wants.
func New(s *tokens.Styler) *Pane { return &Pane{styler: s} }

// SetStyler rebinds the painter when the profile changes.
func (p *Pane) SetStyler(s *tokens.Styler) { p.styler = s }

// SetDismiss binds the close a click outside the panel performs.
//
// It must be the SAME function the esc path calls — the overlay's own close —
// and not a second way of shutting a dialog. That is not a style note: closing
// the settings sheet flushes a debounced write, closing the palette restores
// the keyboard the door took, and a dismissal that skipped either would lose a
// change the reader had already made. Passing the one close makes esc parity a
// property of identity rather than something two paths have to keep agreeing
// about.
//
// A chrome with no dismiss bound still absorbs its clicks and does nothing with
// them, which is exactly the behaviour this package shipped with — so a lane
// that binds the ring and forgets this is no worse off than before.
func (p *Pane) SetDismiss(close func() tea.Cmd) { p.dismiss = close }

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
	p.w, p.h = w, h
	if w <= 0 || h <= 0 {
		return ""
	}
	// The stroke comes from the one renderer that owns §16's ruled lines, so a
	// dialog's boundary and a record's seam are the same mark at the same tier
	// by construction rather than by two files agreeing.
	rule := p.paint(strings.Repeat(blocks.RuleMark, w), tokens.TextTertiary)
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

// -- dismissal (§16 OVERLAY DISMISSAL) ---------------------------------------

// Mouse implements [tui2.PaneMouse]: a click OUTSIDE the panel closes the
// dialog, and every event that reaches this pane is swallowed.
//
// WHY THE RING OWNS THIS. §16 gives dismissal three doors — the `close esc`
// chip, the esc key, and "a click anywhere outside the panel (dialogchrome owns
// 'outside')" — and the third one has to be answered by something that knows
// where the panel ENDS. The panel itself does not: it is handed a rectangle and
// told to fill it, and every coordinate it ever sees is inside its own body.
// The ring is the first cell that is not the dialog, so it is the only pane on
// the surface that can tell the two apart.
//
// WHAT SWALLOWING MEANS HERE. Nothing has to be suppressed: the shell routes a
// click to exactly one layer, the compositor scans from the top of the z order
// down, and this layer sits above the base plane. So a click that reaches here
// has already been taken from the transcript and the rail — returning nothing
// is the swallow. That is the property §16 is really asking for, and it is the
// half a dialog gets wrong most often: a dismissing click that ALSO opened the
// rail row underneath would make "not this" mean "not this, and that instead".
//
// The inside test looks redundant today and is not. It is what makes the pane's
// answer depend on the GEOMETRY rather than on which layer the shell happened
// to route from — so a caller that forwards an outside click from anywhere on
// the frame (see the wiring note in the package doc) gets the same answer, and
// a point that lands where the panel is gets nothing whatever route it came by.
//
// Only the left button dismisses. A wheel notch over the ring is a reader
// scrolling something they cannot see and is dropped; a right click is not a
// verb this surface has.
func (p *Pane) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	click, isClick := msg.(tea.MouseClickMsg)
	if !isClick || click.Button != tea.MouseLeft {
		return nil
	}
	if p.dismiss == nil || local.In(p.panel()) {
		return nil
	}
	return p.dismiss()
}

// panel is the dialog's own body, in this pane's coordinates: the rectangle the
// ring encloses and the overlay plane draws on. An unrendered ring encloses
// nothing, so every point is outside one — which is the right answer for a
// boundary that is not on screen.
func (p *Pane) panel() image.Rectangle {
	if p.w <= 0 || p.h <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(0, 0, p.w, p.h).Inset(ring)
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
