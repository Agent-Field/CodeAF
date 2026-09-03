---
kind: fixed
title: a leaf's room reads the wall it runs under, and a tree that does not build is never settled
pr: 612
surface: [engine, docs]
invalidates:
  - "A leaf's clock was sized from its TOKEN GRANT AND NOTHING ELSE, so it could never learn about time the errand was not spending: `aforge do --timeout 45m` over a plan with one leaf handed that leaf the generalist's fifteen-minute floor and killed it at 46% of the wall with thirty minutes unspent, mid-`edit`, leaving a tree that does not compile. The room now reads the wall through `exec.SubharnessInfo.DeadlineWithin`, which is the subharness table's second door — the table is still the only thing in the process that sizes a leaf's room. The token grant is a FLOOR and the wall a CEILING, so the room only widens: a short wall never takes away room the grant bought, and a surface with no wall (the chat, `aforge exec`, `aforge run`) grants exactly what it always granted. Sizing a leaf DOWN to a short wall is deliberately still not done."
  - "The deadline landing reserve was granted in SECONDS on the very context that was expiring, while the budget reserve is granted in TURNS the loop itself owns — so the turn in flight ate part of it (90 s granted, 61 s left) and the landing's own model call was then cancelled by the clock that had ordered the landing. An ordered landing now runs on a clock of its own, measured from the moment it is ordered and derived from the context the leaf was HANDED, so the leaf's lease expiring cannot take it while the errand's wall and the caller's cancel still can. It is capped by `watchdogPad`, which is the room that pad always said it was for, so a landing lands under the backstop that exists for it. The work is NOT cut short to pay for this: an emitted call still always executes, which is the law that protects the workspace."
  - "`deadlineLandingReserve` capped at a second hand-written `2 * time.Minute`. It caps at `watchdogPad` — the reserve is what a landing is granted and the pad is where the watchdog sits so that landing fits underneath, and they are one bound in two parts."
  - "The arm that ends a leaf on its clock inside a model call set `StopDeadline` and NO `Meter`, alone among every ending in the loop, so a run cut by its own clock had no figures to compose a sentence from and republished the provider's words instead. It sets one."
  - "`settled` was computed from graph terminality alone, and `Failed` is terminal — so a leaf killed mid-edit holding a tree that does not build reported `\"settled\": true` with `\"deliverable\": \"context deadline exceeded\"`. The field keeps the meaning #518 gave it, `nothing this run is waiting for can still move`, and ONE ending answers that sentence false: a run whose own reading of the finished tree could not collect it. Making that tree build is work still waiting to move, however terminal every node is. Such a run is `settled: false`, `ok: false`, `stop: \"incomplete\"`, exit 2. NOTHING ELSE MOVES IT — a project that declares no checks, a reading killed at its ceiling, a suite that ran and went red, an unreadable journal, and the disagreement #518 sanctioned (a run stopped by a question is still `settled: true` with `ok: false` at exit 4) all settle exactly as before. No `--json` field is renamed or removed, and no rung of the exit ladder moves."
  - "The reading that said the tree would not build — `its suite failed to collect, so no check of it ran` — was journalled against the node and reached NEITHER stdout NOR `--json`, so the only way to learn a run had left broken code was `sqlite3` over the run's store. It goes out with the deliverable. It is the LAST word this run's readings said about the finished tree and never the first: a repair round photographs the tree again, and a finding the second reading does not raise stops being a finding."
  - "A failed node's whole deliverable was `strings.TrimSpace(node.Error)`, so a Go error string was published as the answer. A run that did not finish says so first, in a person's words, and the cause follows it."
  - "`\"on the finished tree\"` was a string literal in four places across `internal/exec` and `internal/store`, which is four chances for the writer and the readers of one journal row to disagree about it. It is `store.VerificationWhenFinished`."
---

The two halves are one defect seen from two ends. A clock nobody asked for stopped
the work, and the report of that stop could not say what it had left behind — so
the run read, to every machine caller, as an errand that had settled. Fixing only
the clock would leave the next unlucky stop just as silent, and fixing only the
report would leave a leaf being killed at 46% of a wall the person had set.

The landing reserve is the interesting half. It has existed for a long time and it
was never wrong about how much time a landing needs; it was wrong about who owns
that time. A reserve the work is allowed to spend is not a reserve, and the file
already said so twenty lines above, in the comment on the budget's own staging:
that one is counted in turns precisely because nothing outside the loop can take
turns away. The deadline's is now as un-takeable, by being measured from the moment
it is ordered rather than from what is left of a clock somebody else was spending.
