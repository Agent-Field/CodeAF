// Package blocks is the transcript rendering core of the rebuilt chat surface
// (audit-notes/chat-rebuild.md Part 8.1). It is a pure package: rows in, rows
// out. It imports no Bubble Tea, no lipgloss, and no terminal I/O, so it tests
// headlessly and the golden harness can drive it directly.
//
// The discipline it implements is committed-prefix / live-region, kept INSIDE
// alt screen (8.1.1). A transcript is an ordered list of blocks plus a live
// seam. A block that says it is finalized is rendered exactly once per
// (width, version) and never rebuilt; only the live region rebuilds per frame.
// The unit of correctness is "is this block finalized", not "is this component
// dirty".
//
// The pieces:
//
//   - [Block] — the contract: Rows, IsFinalized, SettledRows, Version, End.
//     [EndState] is the truncation law's rendering hook (12.5): a turn ended by
//     anything other than its own completion renders visibly cut.
//   - [Transcript] — assembly, the one immutable-block cache, the viewport, and
//     the anchor-preserving scroll ported from internal/tui (4.4).
//   - [Clock] — one shared animation clock; every live glyph in the frame
//     derives from one latched instant, so parallel rows animate in lockstep
//     (8.1.3), with fixed-velocity [Shimmer] and eased-dwell [Pulse] (8.1.4).
//   - [TimeCell] — freeze-at-commit for time-derived cells (8.1.2): committed
//     bytes never drift.
//   - [Header] — the one header grammar (8.1.5). Ad-hoc headers are a review
//     reject.
//   - [Folder] — collapse policies that protect the live edge and the failures
//     (8.1.7).
//   - [Styler] — the minimal seam to the token layer. blocks never imports
//     internal/tui2/tokens; the shell injects a Styler.
//
// Memory: a 10k-block transcript holds rendered strings only for the blocks
// inside the retention window (visible ± [Transcript.Window]); everything else
// keeps an int height and a version. See [Transcript.Stats].
//
// Concurrency: a Transcript is not safe for concurrent use. It belongs to the
// render goroutine.
package blocks
