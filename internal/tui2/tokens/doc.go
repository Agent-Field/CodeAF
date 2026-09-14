// Package tokens is the shared token layer for the chat surfaces: the one
// place a colour, a glyph, or a formatted live cell is named.
//
// It is a leaf over the standard library. Nothing in this package imports a
// surface, so every surface above may depend on it and none of it may depend on
// a surface.
//
// Everything here is pure data or a pure function. There is no init function,
// no mutable global, and no I/O. The colour table is built once, at package
// initialization, by one function reading one file's literals — so a render
// never formats an escape sequence, it appends a constant string.
//
// # Why a token layer exists (the August 2026 chat-rebuild audit, no longer in the tree, 10.1.1 and 5.16)
//
// Today's chat renders colour from ad-hoc lipgloss values scattered through the
// render code, so the palette cannot be tested, cannot be degraded for a
// 256-colour terminal in one place, and cannot be proven legible. Here a token
// is a named thing with a base value, a dimmed value, a contrast class, and a
// resolution per terminal capability — and the contrast law is a unit test
// (contrast_test.go) rather than a paragraph of intent.
//
// # The axes
//
// Three axes compose, and they are orthogonal:
//
//   - HUE — what a thing means. Five words and no more (5.16): amber = needs a
//     human, cyan = alive, green = money and success, coral = broken, and a
//     per-task identity pastel from an 8-hue wheel. Everything else is the
//     three-tier grey ramp (5.13). See [Hue].
//   - STATE — how live a thing is (8.1.6): accent = live, plain = settled,
//     dim = chrome. See [State] and [ResolveToken], which is the whole
//     composition rule in one function.
//   - FOCUS — whether the pane owning the row has the user's attention. Every
//     token carries a base value and a dimmed value ([Focus]); dimming is a
//     property of the pane, not of the row.
//
// # The contrast law (the shipping gate)
//
// 5.16 requires every pastel to pass contrast on the default dark ground AND on
// the dimmed variant. That is enforced in code: [Pairings] enumerates every
// legal (foreground, ground) combination the surface may draw, and
// contrast_test.go walks all of them computing WCAG relative luminance. A
// pairing that is not legal is not merely untested — it is a combination the
// surface is forbidden to draw, and the reason is stated at [Legal].
//
// # What the surface packages consume
//
//	tokens.NewStyler(profile, tokens.FocusNormal)
//	tokens.Amber.Fg(profile, tokens.FocusNormal)   // an SGR string, precomputed
//	tokens.Amber.Hex(tokens.FocusNormal)           // "#EECE96", for lipgloss
//	tokens.ResolveToken(tokens.HueBroken, tokens.StateSettled)
//	tokens.GlyphWaitsOn, tokens.Gauge(0.62), tokens.GlyphCut
//	tokens.AppendElapsedCell(buf[:0], d)           // zero allocations
//	sanitize.TextWithPalette(s, sanitize.Table(tokens.ANSI16Remap))
//
// Section numbers in comments refer to the August 2026 chat-rebuild audit, no longer in the tree.
package tokens
