// Package prose renders model-written markdown into styled terminal rows.
//
// It exists because of one line in the 13.3 audit: "questions render raw,
// tables wrap to soup". A model answers in markdown whether or not anyone asked
// it to, and a surface that draws that markdown as literal text is showing the
// reader the model's punctuation instead of the model's answer — `**maybe**`,
// a table folded into ribbons, a fenced block indistinguishable from the
// sentence above it. This package is the one place that text becomes rows.
//
// # The contract
//
//	rows := prose.Render(reply, prose.Options{Width: w, Styler: st})
//
// Every returned string is ONE screen row: it contains no newline, and its
// printable width is at most [Options.Width] cells. That is the same contract
// [blocks.Block] states for Rows, so a caller can hand these straight to the
// transcript — but this package deliberately does not import blocks. The import
// edge in this tree runs tokens → blocks, and a renderer that imported both
// would close a cycle the moment blocks wanted to draw prose. The line builder
// here is small enough to own (see line.go), and owning it keeps the edge
// one-way: prose → tokens → blocks.
//
// # What it does NOT do
//
// It does not use goldmark's HTML renderer and it does not use glamour. Both
// resolve colour themselves, and a second colour authority on this surface is
// exactly the failure internal/tui2/tokens was built to end: a theme that knows
// nothing about the profile ladder, the focus axis, or the contrast gate would
// paint a code keyword in a colour no test in this tree has ever measured.
// goldmark is used for its PARSER only — the AST is walked here and painted
// through [tokens.Styler], so every cell this package draws is a cell the token
// layer named.
//
// # The shape of the output
//
//   - Paragraphs wrap to the MEASURE, not to the terminal. A 200-column window
//     is a wide window, not a wide sentence; [Options.Measure] is the reading
//     length and [Options.Width] is the hard ceiling. Figures — tables and code
//     blocks — get the full width, because a figure is looked at rather than
//     read along, and so does a single token that cannot be broken — a URL, a
//     path, a hash — which is copied rather than read along.
//   - Headings promote by TIER, never by size (5.13). There is no larger type in
//     a terminal, so a heading is louder by standing higher on the grey ramp and
//     by the whitespace around it; h1 also takes bold, and every level past the
//     third steps back down with [tokens.Demote].
//   - Lists hang. A wrapped item aligns under its own first word, never under
//     its marker, so the marker column stays a column.
//   - Tables TRUNCATE. Columns are fitted to the width and over-long cells end
//     in an ellipsis; a cell is never wrapped, so a row is always one row. A
//     table too wide even for its floors keeps that shape and is cut at the
//     right edge — scrollable-shaped, for a caller that can crop — because rows
//     that stay rows can be scrolled, and soup cannot.
//   - Code blocks are highlighted at 256 colours and above and are drawn at the
//     ordinary text tier below (see [tokens.CodeHighlighting]). A lying colour
//     is worse than no colour (5.20).
//
// # Untrusted input
//
// Every byte that reaches this package was written by a model or a tool, so the
// source goes through internal/sanitize with the token layer's ANSI-16 remap —
// the same chokepoint the chat engine, the rail and the homes line use — and
// then loses its SGR sequences entirely. Sanitizing alone would leave a reply
// free to paint itself; prose owns its own colours, so someone else's are
// stripped rather than honoured. Control bytes never reach a cell.
//
// Section numbers in comments refer to audit-notes/chat-rebuild.md.
package prose
