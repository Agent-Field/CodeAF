---
kind: fixed
title: a turn spent watching its own pieces is not a turn to hand over
pr: 304
surface: [engine]
invalidates:
  - "The checkpoint's ladder counted every finished tool batch. A batch drawn entirely from `tasks` and `jobs` — the two windows onto work already out — now counts as watching: it pushes the marks instead of climbing them, exactly as a parked node's deadline is pushed by its parked time."
  - "A mark's sketch split a turn on any two or more parts. It no longer splits on a drawing that is the conversation's own coordination; a part that opens on `wait` and a part whose last stage is a bare `accept`, `approve`, `check` or `review` are hand-backs, and a drawing whose parts are all hand-backs converts nothing."
  - "The sketch ask taught one token, `(done)`. It teaches two: `(waiting)` is the answer for a turn whose only remaining work is waiting on, reading or accepting pieces it already handed out."
  - "A mark that did not split journalled `continue`. A refused conversion now journals `waiting`, so a turn that is one long job and a turn that was only ever watching are no longer the same line in the file."
---

A conversation with four pieces out spent a turn watching them, crossed a checkpoint mark
on the strength of that watching, and had its turn converted into a task twice — each one
briefed to review reports and accept work that a worker in its own copy cannot see, let
alone accept. Both halves are fixed at their one owner: the meter no longer prices a look
at work already out as a round of work, and `checkpointSketch.split` refuses a drawing of
the conversation's own coordination, which is the same answer the mark, the head of the
brief and `drawnDivision.proposes` all read. A verb with something after it — "review the
manuscript" — is still work, and one coordination part beside real ones still splits.
