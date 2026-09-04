---
kind: fixed
title: a leaf knows its own clock, and reading is never the reason one is stopped
pr: 610
surface: [engine, docs]
invalidates:
  - "The guard's fourth signal advises and never concludes: each `noProgressReconTurns` mutation-free span asks the leaf for its result, and each further span asks again with its own larger count. No count of mutation-free turns has ever been able to stop a leaf on `dev`; this pull request adds the notice and not a stop. A leaf whose deliverable is its answer mutates nothing by design, while the repeated-call and stagnant-window signals stop a leaf going in circles at 4 and 6 turns."
  - "`MutationCount` is a per-leaf revision that moves when a write tool files an artifact or when a before-and-after walk of the leaf's own workspace root differs. The walk skips dot entries and dependency and cache directories, is bounded at 6000 entries, and compares endpoints, so a write outside the leaf's directory, a write into a skipped tree, or a file created and deleted inside one shell call never clears the advisory count."
  - "A leaf's brief now names its wall in `--timeout`'s own spelling, and `wallPaceAt` (0.5) buys one live reading of what has gone and what is left. The reading is injected after the deadline landing check, so a leaf already landing is never paced."
  - "PERF.md's leaf-bounds table now carries `noProgressReconTurns` (10), `wallPaceAt` (0.5), and the one-second rounding of the displayed reading. Neither the mutation-free reminder nor the live wall reading may land a leaf."
  - "`internal/manual/chat/adaptive-runs.md` now explains both reminders and says, in bold, that reading is never by itself a reason a worker is stopped. That sentence is now true of the code, including for research, explanations, and reviews whose answer is the result."
---

The measured load is a model that read for forty minutes with line numbers in
its prompt and an explicit instruction to start writing. A reminder alone may
not move that worker, but the absence of a write cannot distinguish it from a
researcher whose work belongs in the reply. The wall owns the first run's end;
repeated calls and already-seen results own the no-progress stop.
