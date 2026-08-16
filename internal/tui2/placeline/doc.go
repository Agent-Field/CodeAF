// Package placeline is the place line of chat-rebuild 5.19: the dim words
// answering the always-missing question the breadcrumb never does — "where is
// THIS work happening on disk", the pwd a terminal usually gives away for free
// and our chrome had been silent about.
//
// It was a ROW on the composer's top edge for one wave. §7's hug folded it into
// the bottom bar's right zone, beside the context gauge and the day's spend, and
// what survives here is the part that was worth keeping: the GRAMMAR — fish
// abbreviation, tilde folding, `·` separators, legs dropped from the left under
// an overflow mark. [Model.Render] still draws the row for any surface that
// wants one; [Model.Text] hands the same fitted words to a surface that will
// place and paint them itself, which is what the bar row does.
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
// oldest context — a dropped prefix is marked with [tokens.GlyphEllipsis],
// never [tokens.GlyphTruncated], which means something clickable elsewhere and
// would lie here) and, only once a single remaining leg still does not fit
// alone, by middle-ellipsizing that leg's own text. This is the reverse of
// [blocks.Header]'s own degrade order (which drops from the right) because the
// two rows carry opposite priorities: a header's newest information is on the
// LEFT (the glyph, the title); a place line's is on the RIGHT (the most
// specific, most current segment of where work is actually landing).
//
// # The two glyphs, and where they come from
//
// 5.19's grammar calls for `⌂` marking a task-workspace leg, and for a
// static "…" where legs were dropped. Both are slots in the shared table now
// — [tokens.GHome] and [tokens.GlyphEllipsis] — and this file reaches for
// the slots rather than for the bytes.
//
// That is a correction, and worth recording as one. This section used to say
// tokens did not carry `⌂`, and it was right when it was written: the place
// line drew the character itself. The cost was invisible until the repertoire
// tier landed — a locally spelled glyph can only ever draw the PLAIN floor, so
// this was the one surface in the product that stayed plain when a reader
// turned nerd fonts on, while every column around it swapped. Asking
// [tokens.Styler.Glyph] for the SLOT gets the tier for free, and both sides of
// the binding are one cell (asserted by the tokens width gate over both tiers),
// so [fit]'s arithmetic does not change.
//
// The overflow mark stays deliberately distinct from [tokens.GlyphTruncated],
// which means something clickable elsewhere and would lie here.
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
