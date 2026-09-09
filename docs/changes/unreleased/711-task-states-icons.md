---
kind: changed
title: every mark a person sees comes from one vocabulary, in the tier their terminal can draw
pr: 711
surface: [chat, docs]
invalidates:
  - "The chat had three glyph vocabularies. internal/tui2/tokens held one, internal/tui3/actionicon.go held a private three-tier table with ten Nerd Font private-use codepoints spelled inline, and the task states were seven constants spread over six files. There is one now, in internal/tui2/tokens, and internal/tui3/iconvocab_test.go fails the build on a mark spelled anywhere else."
  - "The task-state marks were `◌` queued, `▸` working, `✓` done, `⊘` stopped, `✗` incomplete, `?` your call, `⏸` paused at the cap. They are `○` queued, `⚑` waiting on a sibling or on the machine, `◐` working, `✓` done, `■` stopped, `✕` incomplete, `?` your call, `=` paused - one shape per state, and the shape alone says the state with no colour. A row held behind another task used to wear the queued circle; it wears the flag now, because it is blocked rather than queued."
  - "`⏸` was still drawn on a run standing at its spend gate, although tokens.BannedGlyphs has refused it since 5.17 for measuring two cells in half the fonts that carry it. Nothing draws it any more."
  - "internal/tui3 no longer declares glyphQueued, glyphRunning, glyphDone, glyphBad, glyphStopped, glyphAsk, glyphPaused or their ASCII twins. tasktier.go's tierSlot maps a reading to a tokens.GlyphID, and palette.glyph / app.icon are the only doors to a character."
  - "tokens.GlyphSet had two tiers, Plain and NerdFont. It has three: ASCII is the screen reader's, one character it can name per icon slot, and GlyphSet.Glyph(id) is the one door to all three. tokens.DetectGlyphSet still never returns ASCII - that tier is asked for by name, by the linear option."
  - "tokens gained GlyphStopped (`■`, nf-fa-stop) for work a person ended, and bindings for the ten action families that were not already slots. Every one is measured by the same width, ban, provenance and one-meaning gates as the rest of the table."
  - "The manual said `◌`, `⊘`, `✗` and `⏸` for those states; it says `○`, `■`, `✕` and `=`, and tasks.md now tells a reader that a patched font draws icons and that /settings step icons is where to turn them off."
  - "Home drew `◌` for a conversation left unfinished. It draws `✕`, the mark work that did not finish wears on every other screen."
  - "The tmux suite's throwaway profile pins `plain` in the Display row, because a patched terminal draws private-use codepoints and a capture-pane of one is not a thing a needle could honestly assert."
---

The owner's ruling: a person with a patched font was seeing proper icons beside
their tool calls and bare geometric shapes beside their tasks, because a mark
spelled as a literal draws the plain floor forever - it cannot know which
repertoire the terminal is on. docs/design/icons/DESIGN.md is the law, the table
as it landed, and how to add a mark.
