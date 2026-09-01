---
kind: changed
title: one top bar for chat and task, and the bottom folded to two rows
pr: 100
surface: [chat]
invalidates:
  - "The chat had no header — no longer true. Every conversation now wears a top bar: `· project › chat name` on the left, `model[:effort] · branch* · host · YOLO` on the right, with a hairline rule under it. It costs the body two rows, and on a frame too short for the body to keep a row it is the first chrome to go."
  - "The task strip existed — no longer true. The row of chips above the transcript is gone and nothing replaces it: the crumb says the place, the roster carries the running set, `ctrl+t`/`ctrl+g` open it, and the phone tier's deck draws the live set. Its phone form — one full-width `▸ 4 tasks · 2 running` door — went with it."
  - "The room header was a task-only surface — no longer true. Its facts moved up into the top bar's task form: the node glyph, the crumb `project › chat name › task title #N`, the state, the clock, the spend, and the `esc/← back · ✕` way out. The dim kin rows under it are gone too; the crumb carries the parent."
  - "`$0.00` was the stated exception to the emptiness law — no longer true. Zero spend draws no segment at all; the row ends at the numbers. `dollars()` itself still spells a zero, because a gauge with a tank and a ceiling somebody typed are figures where `$0.00` is the true reading — what refuses is the row (`render.go`'s `costSegment`), and `CLAUDE.md`'s stated law moved with it."
  - "`idle` was a status word — no longer true. An idle chat's status row simply ends at the numbers."
  - "model, branch and host lived at the bottom of the screen — no longer true. They are the top bar's right cluster, dim at rest and brightening under the pointer; the bottom row carries only the ticking facts. YOLO and the reconnect note moved up with them."
  - "The status row's left end was the identity cluster — no longer true. It is the presence clauses (`N jobs · N watch`, `◦ keeping an eye on N`), dim; the identity is the crumb."
  - "The legend's left end carried `host · branch` — no longer true. It is empty at rest, and the generic key advertising (`space space home · tab last · ctrl+k switch · / commands`) now decays with the earned tips: the advertisement quiets, never the doors."
  - "The status line carried the whole context meter and its sparkline — no longer true. The row is a percent alone, the full `12.4k/128k · 10%` form comes back automatically past 80% of the compaction threshold, and the sparkline is `/status` and the status sheet only."
  - "The phone deck's first row named the conversation — no longer true. It carries the crumb, walked down the same ladder the bar's is: `project › chat` at rest, the room's own chip in a room."
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
step is never sacrificed for a parent. It does not outrank the safety posture
or the mark that stops the work either: at the last width the crumb yields and
`YOLO` and the `✕` stand. The run page's own trail walks the same ladder.

The bottom is two rows plus the composer. The status row keeps only the
ticking facts, right-aligned in a fixed order — `N open · M want you`, the bill,
the context percent, the compaction estimate, the state word last — with the
presence clauses (`N jobs · N watch`, `◦ keeping an eye on N`) dim at the left.
Everything else moved: the crew, the delta, the cache, the burn rate and the
served rider are one press away in the deck's sheet and in `/status`, which is
what a sheet is for. Every segment is now routed to its surface by name and a
new one with no lane stops the render, which is how the six that used to fall
through a switch with no default were found.

And the numbers are doors. The bill opens Spending, the percent prints
`/status` into the transcript, the count opens the conversations list, `YOLO`
opens the Settings page's Safety tab, the crumb's project step opens home and
its chat step leaves the room however deep the trail is — while the
`esc/← back` word beside it climbs exactly one level, which is why the two are
separate doors that light separately. Every one of them lights at the size of
its own span, never the row it rides.
