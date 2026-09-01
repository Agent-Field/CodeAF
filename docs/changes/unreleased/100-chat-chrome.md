---
kind: changed
title: one top bar for chat and task, and the bottom folded to two rows
pr: 100
surface: [chat]
invalidates:
  - "The chat had no header — no longer true. Every conversation now wears a top bar: `· project › chat name` on the left, `model[:effort] · branch* · host · YOLO` on the right, with the legend's hairline rule under it."
  - "The task strip existed — no longer true. The pinned rows under the room header are gone; what is running is the top bar's glyph and the status row's state word, and the phone tier's deck carries the live set."
  - "The room header was a task-only surface — no longer true. Its facts moved up into the top bar's task form: the node glyph, the crumb `project › chat name › task title #N`, the state, the clock, the spend, and the `esc/← back · ✕` way out."
  - "`$0.00` was the stated exception to the emptiness law — no longer true. Zero spend draws no segment at all; the row ends at the numbers."
  - "`idle` was a status word — no longer true. An idle chat's status row simply ends at the numbers."
  - "model, branch and host lived at the bottom of the screen — no longer true. They are the top bar's right cluster; the bottom row carries only the ticking facts."
  - "The status row's left end was the identity cluster — no longer true. It is the presence clauses (`N jobs · N watch`, `◦ keeping an eye on N`), dim; the identity is the crumb."
  - "The legend's left end carried `host · branch` — no longer true. It is empty at rest, and the generic key advertising (`space space home · tab last · / commands`) decays with the earned tips."
---

The chat chrome was two designs wearing one frame: a task got a pinned header
and a strip of running work, and a conversation at rest got neither — the
place you were in was a fact only the roster knew, and the model, branch and
host were a line at the bottom that had to be found before it could be read.

Now there is one top bar for both. A conversation says `· project › chat name`
with its terms at the right end; a task says the same thing one level deeper,
`⠙ project › chat name › port the lexer #2 · working · 2m12s · $0.04`, with the
whole left cluster lit and the way out (`esc/← back · ✕`) at the right. The
crumb gives way in a fixed order — middles fold to `…`, then the project goes,
then the parent, then the title truncates to twelve columns — and the current
step is never sacrificed. The run page's own trail walks the same ladder.

The bottom is two rows plus the composer. The status row keeps only the
ticking facts, right-aligned in a fixed order — `N open · M want you`, the bill,
the context percent, the compaction estimate, the state word last — with the
presence clauses (`N jobs · N watch`, `◦ keeping an eye on N`) dim at the left.
Everything else moved: the crew, the delta, the cache, the burn rate and the
served rider are one press away in the deck's sheet and in `/status`, which is
what a sheet is for. And the numbers are doors now — the bill opens Spending,
the percent prints `/status` into the transcript, the count opens the
conversations list, YOLO opens the Settings page's Safety tab, and every step
of the crumb climbs one level toward home.
