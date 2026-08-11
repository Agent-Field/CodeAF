// Package placeline is the place line of chat-rebuild 5.19: one dim row on
// the composer's top edge answering the always-missing question the
// breadcrumb never does — "where is THIS work happening on disk", the pwd a
// terminal usually gives away for free and our chrome had been silent about.
//
// # Shell-prompt familiar, not shell-prompt literal
//
// The line is written in the oh-my-zsh / p10k lineage 5.19 names: fish-style
// path abbreviation, `·` segment separators, dim tier. It deliberately drops
// the lineage's powerline triangles (5.21's anti-catalog: font-fragile) and
// its multi-color segment fills (5.16: everything not carrying one of the
// five hues is the grey ramp) — a familiar SHAPE, not a familiar look.
//
// # The ground is a chain of legs, not one string
//
// [Segment] is one leg of the ground and [SegmentKind] says what kind: the
// resident root the surface itself lives in, a task's own workspace (which
// sits wherever it sits on disk — a task workspace is not nested under the
// resident root, 5.19's own example puts `/tmp/wisp-parity` beside
// `~/aforge-v2`, not under it), or a worker's region inside its task's
// workspace. Only the root is fish-abbreviated; a workspace or a region IS
// the material ground the whole feature exists to name, and 5.19's own law —
// "never tail-truncate a path — the filename is the information" (5.21) —
// means those two never lose a letter to compactness, only to width pressure,
// and only from the middle ([blocks.TruncatePath]).
//
// # Degrading under width pressure
//
// The line shortens by dropping LEGS from the left first (the broadest,
// oldest context — a dropped prefix is marked with a plain, static "…", never
// [tokens.GlyphTruncated], which means something clickable elsewhere and
// would lie here) and, only once a single remaining leg still does not fit
// alone, by middle-ellipsizing that leg's own text. This is the reverse of
// [blocks.Header]'s own degrade order (which drops from the right) because the
// two rows carry opposite priorities: a header's newest information is on the
// LEFT (the glyph, the title); a place line's is on the RIGHT (the most
// specific, most current segment of where work is actually landing).
//
// # A glyph tokens does not carry yet
//
// 5.19's grammar calls for `⌂` marking a task-workspace leg. [tokens.Glyphs]
// does not carry it (checked directly against the package this file was
// written beside) — a gap in the shared vocabulary, not a decision to route
// around it. [glyphWorkspace] is defined locally, measured the same way
// tokens.go measures its own table (one cell under x/ansi and go-runewidth,
// in both default and CJK-locale east-asian-width modes — narrower a margin
// than several glyphs tokens already ships, e.g. its own `·` and `○`, which
// are Ambiguous-width and shipped anyway) and is a candidate for a future
// amendment to tokens' table, in the spirit of that package's own "AMENDMENT
// TO 5.17" note on ⚡.
//
// # Contract with the shell
//
// [Model] is built with [New] and configured with [Model.SetGround] and
// [Model.Focus]; [Model.Render] is a pure function of that state and a given
// width, returning at most one row, never panicking and never exceeding the
// width it was given, down to w=1. [Model.CopyText] is the y-copy affordance
// 5.19 promises ("click/y copies") — wiring it to an actual key is a later
// wave's job; this package only has to know what the answer is.
//
// Section numbers in comments refer to audit-notes/chat-rebuild.md.
package placeline
