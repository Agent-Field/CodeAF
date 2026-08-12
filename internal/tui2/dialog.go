package tui2

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

// The floating dialog's boundary, drawn.
//
// layout.go's dialog-anatomy note settles WHAT the boundary is and why it is not
// a border; this file is the one place that boundary becomes cells. Two
// characters wide vocabulary: a space and a hairline.
//
// THE STROKE IS NOT RESTATED HERE, and it used to be. This root package cannot
// import the tokens sibling without inverting the tokens → tui2 dependency (see
// the SEAM note in metrics.go), so the hairline shipped as a bare literal with a
// test pinning it to [tokens.GlyphTreeDash] from outside. It no longer has to:
// §16 admits exactly two ruled lines and internal/tui2/blocks owns the renderer
// for both, blocks is a LEAF this package may import freely, and its
// [blocks.RuleMark] is already twinned to the token vocabulary by a test of its
// own. One mark, one owner, and one fewer restatement to keep honest.
// TestDialogChromeDrawsTheTokenHairline (dialog_tokens_test.go) still drives the
// real shell and still measures what a terminal receives.
//
// SEAM — the chrome is drawn UNPAINTED, at the structural tier only. Colour is
// the token layer's to give, and a caller that wants it tinted binds its own
// pane to [LayerDialogChrome] exactly as it binds every other layer — the shell
// draws this the way it draws a placeholder, as the honest default for a region
// nobody has claimed. Unpainted is also the profile-independent answer: the
// hairline is the boundary at `--color none` and at truecolor alike, which is
// the degradation ladder 10.1.2 asks for rather than a fallback nobody tested.

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
	rule := blocks.Rule(w, nil)
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
