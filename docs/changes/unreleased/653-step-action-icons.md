---
kind: added
title: each compact step carries one still action icon in a fixed gutter
pr: 653
surface: [chat, engine, docs]
invalidates:
  - "The compact working block drew step descriptions alone. Each of its three rows now leads with one monochrome mark naming the kind of work — search, read, edit, create, run, test, browse, transfer, communicate, coordinate, plan, wait, or the generic work bucket — in a fixed two-column gutter."
  - "The previous wave stated that no icon collection is introduced. Thirteen action marks now exist; they are still, never a verdict, and they do not replace the shimmer, the spinner, the tool-row state marks or any count."
  - "The caption narrator answered with a sentence only. It now answers `run | starting the local server` — one word from a closed list, a bar, then the same free sentence — on the same role, the same three-per-turn bound and the same 80-token ceiling, with no second model call."
  - "session.Event carried no notion of what kind of work a caption was about. EventCaption now carries Category beside Text; it is omitted from the wire when empty, and a peer without it is served exactly as before."
  - "A step's narration lived only in the live event hub and was lost when the window closed. It is journaled as a `caption` line and replayed onto the batch it names, so a reopened conversation reads the sentence a person was reading and draws the same mark — rather than recomposing `running 1 command` and demoting a narrated `test` to the `run` the tool name implies. session.DisplayEntry carries Caption and CaptionCategory; a file with no `caption` lines replays exactly as it always did."
  - "EventCaption named its step by recency and a surface keyed it onto the newest tool row of the turn. It now carries the batch's first call id in Event.CallID and a surface keys on that: the narrator can be descheduled past the end of its own batch, and a late answer used to retitle whichever step was running when it landed. An event with no anchor is an older engine and keeps the old rule."
  - "SplitActionLine handed back the raw line for a format attempt with no sentence behind it, so `run |` reached the caption row as a step title. Both halves now come back empty and the deterministic composite stands."
  - "internal/tui3's feed drew a row for a tool that began without streaming its call and recorded no call id on it. It records ev.CallID there like every other door onto a tool row, so ends, figures and captions can pair on it."
---

The mark is derived from the tools before any narration arrives, so it is
correct from the first frame; a valid narrated family replaces the sentence and
the mark in one repaint. A missing, malformed or unknown family keeps the clean
caption and the tool-derived mark, with no retry and no second call. Repainting
cannot change a mark, and reopening cannot either — the narration is on the
record, and where there is none the tool-derived floor answers as it always did.

The caption is accepted once for both the journal and live event, against the
same batch anchor. An answer already cancelled when it returns reaches neither.
Once accepted, both paths carry the same words and category even if the batch
ends immediately afterward. Detached transcript readers retain those fields too.

The normal presentation uses proper Font Awesome icons from the Nerd Font
repertoire. Detection falls back on known unsupported terminals; Display's
`step icons` setting (`ui.icons`) offers `auto`, `rich`, and `plain` and applies
immediately. Plain Unicode and ASCII retain the same gutter. Colour depth alone
does not remove icons, and no font is installed or changed automatically.

No tool count, badge or verdict mark is added. The later step-time change adds elapsed time after 10 seconds. `test` draws a flask (a target in plain mode) and
never a checkmark, because the family names the act of checking rather than its
result. Completion still collapses the block, and the opened outline keeps its
disclosure triangles unchanged.
