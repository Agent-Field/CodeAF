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

	// RailSlimWidth is the collapsed rail's column count: the handle.
	//
	// It is a SECOND width rather than a flag because the solver's whole job is
	// arithmetic — a rail that is sometimes 28 columns and sometimes 1 is one
	// number with two values, and everything downstream (the seam, the main
	// column, the narrow fallback) already reads that number. Zero means a
	// surface that has no handle rendering, and the slim rung then collapses to
	// hidden rather than reserving a column nothing draws in.
	RailSlimWidth int

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

	// DialogFullscreenBelowWidth is the width under which a dialog stops being
	// a centered panel and takes the whole frame.
	DialogFullscreenBelowWidth int

	// DialogFullscreenBelowHeight is the height under which a dialog stops
	// being a centered panel and takes the whole frame. Below it there is no
	// room to float a panel over three rows of transcript, which is the same
	// "nothing left to float over" reasoning the width door uses — see
	// consentui.ForcedFullscreen, which forces on both axes for the same
	// reason.
	DialogFullscreenBelowHeight int

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
//
// The two DialogFullscreenBelow* numbers are the exception: they are not
// starting numbers, they are pinned. tokens.DialogFullscreenBelowWidth (72)
// and tokens.DialogFullscreenBelowHeight (20) are the 10.5.24 table's answer
// to "forced fullscreen below a stated size threshold" (10.4.17), and this
// root package cannot import the tokens sibling without inverting the
// dependency the tokens → tui2 seam is built on (see the SEAM note above), so
// the values are restated here as literals rather than read off the constant.
// TestDefaultMetricsAgreesWithTokenBreakpoints (metrics_tokens_test.go) pins
// the two literals to the tokens constants from outside this package, so
// drift between the two fails a test instead of shipping quietly.
func DefaultMetrics() Metrics {
	return Metrics{
		RailBreakpoint:              100,
		RailWidth:                   28,
		RailSlimWidth:               1,
		MinMainWidth:                56,
		ComposerHeight:              3,
		StatusHeight:                1,
		DialogFullscreenBelowWidth:  72,
		DialogFullscreenBelowHeight: 20,
		SplitDiffBreakpoint:         120,
		PasteToAttachmentLines:      8,
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
	if m.RailSlimWidth < 0 {
		m.RailSlimWidth = 0
	}
	if m.RailSlimWidth > m.RailWidth {
		// A handle wider than the column it stands in for is not a handle. The
		// clamp is here rather than at the call site because the table is data
		// and data arrives wrong eventually (see this function's own contract).
		m.RailSlimWidth = m.RailWidth
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
