package tui2

import "strings"

// The floating dialog's boundary, drawn.
//
// layout.go's dialog-anatomy note settles WHAT the boundary is and why it is not
// a border; this file is the one place that boundary becomes cells. Two
// characters wide vocabulary: a space and a hairline.
//
// SEAM — internal/tui2/tokens: the hairline is restated here as a literal rather
// than read off [tokens.GlyphTreeDash], for the same reason DefaultMetrics
// restates the two fullscreen breakpoints (see the SEAM note in metrics.go) —
// this root package cannot import the tokens sibling without inverting the
// tokens → tui2 dependency. TestDialogChromeDrawsTheTokenHairline
// (dialog_tokens_test.go) pins the restatement to the constant from outside the
// package, so drift fails a test rather than shipping quietly.
//
// SEAM — the chrome is drawn UNPAINTED, at the structural tier only. Colour is
// the token layer's to give, and a caller that wants it tinted binds its own
// pane to [LayerDialogChrome] exactly as it binds every other layer — the shell
// draws this the way it draws a placeholder, as the honest default for a region
// nobody has claimed. Unpainted is also the profile-independent answer: the
// hairline is the boundary at `--color none` and at truecolor alike, which is
// the degradation ladder 10.1.2 asks for rather than a fallback nobody tested.
const dialogHairline = "─"

// dialogChrome renders the margin ring at a size: a hairline rule along the top
// and bottom edge (5.13's room boundary) and blank ground between them. The
// panel is drawn on the plane above and covers everything inside the ring, so
// what survives on screen is one column of ground on each side and one ruled
// row above and below.
//
// It draws no corners, no side rules and no title bar. A rule top and bottom is
// two hairlines at a room boundary; add the two verticals and it becomes the
// box-drawing frame 5.21's anti-catalog refuses.
func dialogChrome(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	rule := strings.Repeat(dialogHairline, w)
	if h == 1 {
		return rule
	}
	var out strings.Builder
	out.Grow((w + 1) * h)
	out.WriteString(rule)
	blank := strings.Repeat(" ", w)
	for row := 1; row < h-1; row++ {
		out.WriteByte('\n')
		out.WriteString(blank)
	}
	out.WriteByte('\n')
	out.WriteString(rule)
	return out.String()
}
