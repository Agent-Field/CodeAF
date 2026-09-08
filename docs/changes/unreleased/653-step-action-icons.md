---
kind: added
title: each compact step carries one still action icon in a fixed gutter
pr: 653
surface: [chat, engine, docs]
invalidates:
  - "The compact working block drew step descriptions alone. Each of its three rows now leads with one monochrome mark naming the kind of work — search, read, edit, create, run, test, browse, transfer, communicate, coordinate, plan, wait, or the generic work bucket — in a fixed two-column gutter."
  - "The previous wave stated that no icon collection is introduced. Thirteen action marks now exist; they are still, never a verdict, and they do not replace the shimmer, the spinner, the tool-row state marks or any count."
  - "The caption narrator answered with a sentence only. It now answers `run | starting the local server` — one word from a closed list, a bar, then the same free sentence — on the same role, the same three-per-turn bound and the same 80-token ceiling, with no second model call."
  - "session.Event carried no notion of what kind of work a caption was about. EventCaption now carries Category beside Text; it is omitted from the wire when empty, and a peer or a saved conversation without it derives the same mark from the batch's own tool names."
---

The mark is derived from the tools before any narration arrives, so it is
correct from the first frame; a valid narrated family replaces the sentence and
the mark in one repaint. A missing, malformed or unknown family keeps the clean
caption and the tool-derived mark, with no retry, and the existing cancellation
rule still refuses an answer that arrives after its batch ended. Repainting,
reopening and replaying a conversation cannot change a mark.

Every glyph draws in an ordinary monospace font — four are slots the shared
`internal/tui2/tokens` vocabulary already ships and measures — and each has a
one-cell ASCII twin for the screen-reader tier. **The patched-font tier is
deliberately not wired here:** `tokens.DetectGlyphSet` defaults to NerdFont and
can only veto, so wiring it would draw tofu in this gutter on every unpatched
terminal. Rich icons remain available to a later wave behind an explicit opt-out
door this surface does not yet have.

No tool count, timer, badge or verdict mark is added. `test` draws a target and
never a checkmark, because the family names the act of checking rather than its
result. Completion still collapses the block, and the opened outline keeps its
disclosure triangles unchanged.
