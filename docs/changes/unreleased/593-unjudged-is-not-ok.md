---
kind: fixed
title: A delivery the gate could not judge is never ok — it is its own ending, said in words
pr: 593
surface: [engine, docs]
invalidates:
  - "A headless run whose delivery gate could not be reached ended `ok` at exit 0 with NO gate row in the store at all. The gate is fail-open — a judge that cannot answer must not hold a finished deliverable hostage — and for as long as that shipping was journaled by a line in the log alone, a delivery nobody had read was indistinguishable at every later surface from one that had been read and found whole. `revision.Judgment.Unjudged` existed and had no reader anywhere. Two graded runs on aforge-v2-14's rig (reef-145, the `aforge do` door) delivered that way on 2026-09-02: the repair leaf finished its work, the gate's calls came back 404, do.err carried `the gate could not be reached; delivering unjudged`, the node showed `✓`, the door said `ok`, and the rig compared both against runs that had actually been judged. NOW: the delivery still ships, and it gets a gate row of its own kind — `store.DeliveryGate.Unjudged`, which `Whole()` spends — the run ends `unchecked` at exit 2 rather than `ok` at 0, the last line on the door reads `delivered without a check: the gate could not be reached` with the reason after a `·`, and `aforge do --json` carries the whole reason in a new `unjudged` field. The store refuses a row that claims both a pass and no judgement."
  - "`stop` had eight words. It has nine: `unchecked` is a run that delivered and nothing judged what it delivered. It sits on the exit-2 rung beside `incomplete`, so the number a script reads is unchanged and the word tells the two endings apart — `incomplete` is a check that was made and came up short, `unchecked` is a check that never happened."
  - "A gate call refused on the wire was asked once and never again. It is now asked ONE more time, inside the run's own wall, and the note says which happened: `asked twice`, `the wall left no time for a second call` (under 30 s of wall left), or `the request itself was refused, so asking again would say the same` — that last read off `provider.APIError.OurRequest`, the same shape internal/exec and internal/session already retry on, so a router's own 4xx is not retried and an upstream's is."
---

The gate has always been fail-open, and that was the right call: a judge that
cannot be reached knows nothing about the work, and holding a finished
deliverable hostage to the weather helps nobody. What was wrong was that the
shipping was silent. The pass a fail-open gate hands out looked, at every surface
downstream of it, exactly like a pass a judge had read the deliverable to reach —
same `✓`, same `ok`, same exit 0, and on two measured runs not even a row in the
store to tell them apart afterwards.

So the fail-open pass now says so, all the way out: one row of its own kind in
the journal, one word of its own in `stop`, one sentence of its own on the door.
A person who reads `delivered without a check: the gate could not be reached`
knows both things they need to know — the answer above them is theirs to keep,
and nothing has vouched for it.
