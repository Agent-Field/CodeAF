package tui2

// The responsive numbers table (10.5.24). Crush's lesson, adopted: the widths
// at which the surface changes shape are written down before anything is built
// on top of them, so a breakpoint is a decision with a name rather than a
// magic number discovered later inside a render function.
//
// These numbers belong to the tokens sibling. Until it lands, the shell reads
// the provisional table below — chosen to agree with what the old surface
// already does, so the two do not disagree about the same terminal. The seam
// is deliberately a plain struct rather than an interface: the shell reads
// these values once per layout, and a value type costs nothing to copy and
// nothing to keep alive.
//
// SEAM — internal/tui2/tokens: when it lands it imports this package for the
// type and returns a filled Metrics (or cmd/aforge adapts its own struct into
// one). The dependency runs tokens → tui2, never the other way, so the root
// package stays importable by every sibling without a cycle.

// Metrics is the width- and height-dependent table the layout consults. Every
// field is in terminal cells.
type Metrics struct {
	// RailBreakpoint is the total width at or above which the scope rail is
	// drawn beside the transcript. Below it the same scope rows render as a
	// full pane (5.15): the rail is a rendering of scope, not scope itself.
	RailBreakpoint int

	// RailWidth is the rail's column count when it is drawn.
	RailWidth int

	// MinMainWidth is the narrowest the transcript column may become before
	// the rail gives up its columns entirely. Without this a wide-but-not-wide-
	// enough terminal ends up with two unusable panes instead of one good one.
	MinMainWidth int

	// ComposerHeight is the composer's row count, border included.
	ComposerHeight int

	// StatusHeight is the status line's row count. It is the last thing the
	// layout gives up, because a surface with no status line cannot tell the
	// user why it looks the way it does.
	StatusHeight int

	// DialogFullscreenBelow is the width under which a dialog stops being a
	// centered panel and takes the whole frame. Wave 3 consumes it.
	DialogFullscreenBelow int

	// SplitDiffBreakpoint is the width at or above which a diff renders as two
	// columns rather than unified. Wave 3 consumes it.
	SplitDiffBreakpoint int

	// PasteToAttachmentLines is how many pasted lines become an attachment
	// instead of composer text. Wave 3 consumes it.
	PasteToAttachmentLines int
}

// DefaultMetrics is the provisional table. RailBreakpoint matches the old
// surface's railAtWidth so the two windows agree about when a terminal is
// wide; everything else is a starting number the tokens sibling may replace.
func DefaultMetrics() Metrics {
	return Metrics{
		RailBreakpoint:         100,
		RailWidth:              28,
		MinMainWidth:           56,
		ComposerHeight:         3,
		StatusHeight:           1,
		DialogFullscreenBelow:  80,
		SplitDiffBreakpoint:    120,
		PasteToAttachmentLines: 8,
	}
}

// sane returns the table with every field forced into a range the layout can
// actually solve. A token table is data, and data arrives wrong eventually; a
// negative composer height must produce a smaller composer, never a panic.
func (m Metrics) sane() Metrics {
	if m.RailBreakpoint < 0 {
		m.RailBreakpoint = 0
	}
	if m.RailWidth < 0 {
		m.RailWidth = 0
	}
	if m.MinMainWidth < 0 {
		m.MinMainWidth = 0
	}
	if m.ComposerHeight < 0 {
		m.ComposerHeight = 0
	}
	if m.StatusHeight < 0 {
		m.StatusHeight = 0
	}
	return m
}
