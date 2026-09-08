---
kind: fixed
title: a leaf's landing reserve is bounded in tokens as well as turns, so a spent grant cannot buy another
pr: 624
surface: [engine, docs]
invalidates:
  - "`landingTurns` (4) was the WHOLE reserve a leaf got when it spent its token grant, and nothing measured what those four turns cost. On the worker trial of 2026-09-03 one reserve billed 122,392 tokens against a 150,000-token grant — 82% of the grant, spent after the grant was declared spent — and four leaves ended at 1.18x, 1.28x, 1.74x and 2.04x of what they were given. The reserve now ends on whichever half arrives first: `landingTurns`, or `landingTokenShare` (0.2) of the grant measured from the spend at the crossing. A leaf whose turns are cheap still gets all four."
  - "`--token-budget 150000` did not mean a run could spend about 150,000 tokens; it reliably bought around 300,000. The effective ceiling is now stated rather than discovered — the grant, plus a fifth of it for the landing, plus overshoot from both the turn that crossed the grant and the final landing turn. It is not exactly the grant and cannot be: the loop learns a turn's bill only after the turn is complete, and that last turn is what keeps a mid-edit workspace whole. `PERF.md`'s leaf-bounds table and `docs/HEADLESS.md`'s `--token-budget` row both say so now."
  - "A landing is never cut to nothing. The token half is read AFTER a turn has been billed rather than before one is bought, so an exhausted leaf always gets the one whole turn that leaves the tree consistent, even when that turn is itself larger than the whole allowance. What it cannot do any more is take three more like it."
  - "The deadline landing is unchanged and is NOT bounded by this allowance. It was never the unbounded reserve: `deadlineLandingReserve` already bounds it with a clock. Only the budget landing sets a token ceiling, and `landingCeiling` is zero on every other path."
  - "Nothing about how an exhausted leaf is detected, recorded or reported moved. `spent()` is still the counter, `exhausted()` still fires on the turn that crosses, `Outcome.Exhausted` is still `StopBudget`, the `leaf_exhausted` row is still journaled, and `Outcome.Meter.Reached` is still re-read at land time so the ⏳ line counts the landing too. The figures on that line simply get smaller."
---

The check was never the defect. A stop happened, a line was printed, and both
said the honest number — which is how the overrun was found at all. What was
missing is that the reserve granted after the stop was a count of iterations, and
a count of iterations is not a bound on spend when one iteration can cost a
quarter of the grant.

The merge with current dev preserves the independent bounded landing clock and
repeated-timeout guard alongside the token allowance; each retains its own
existing threshold and ending ownership.
