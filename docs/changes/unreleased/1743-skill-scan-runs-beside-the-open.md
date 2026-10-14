---
kind: changed
title: the foreign-skill scan runs beside a new conversation instead of inside it
pr: 1743
surface: [chat, resident]
invalidates:
  - "Opening a conversation used to scan every foreign skill folder synchronously before the first message could build, on every road — on a shared connection inside the keystroke itself. The scan runs in the background now; the first message waits up to two seconds for it, so it still sees skills installed for another harness, and no later message ever waits at all."
  - "The background scan and the resident reconciler's own scan serialize behind one lock, so the two runners never walk the same disks at once or contend the shelf's writes."
---

The foreign-skill import pass is no part of opening a conversation anymore.
The launch returns immediately; the pass walks the skill folders beside it
and closes a gate the first turn's skill resolve waits on, bounded, so the
very first message keeps seeing the whole shelf while typing, enter and every
later message wait for nothing. A shutdown that wins the race against the
scan costs the scan itself — the resident reconciler runs the same pass on
its own clock, so nothing is lost.
