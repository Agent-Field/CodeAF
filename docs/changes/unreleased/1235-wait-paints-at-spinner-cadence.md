---
kind: fixed
title: a wait whose stream has stopped arriving paints at the spinner cadence, not thirty frames a second
pr: 1235
surface: [chat]
invalidates:
  - "The paint clock ran at its full 33 ms cadence whenever any liveness term held, the wait on a model included, so a turn in flight with nothing arriving but a spinner cost the same as one streaming text. A quiet wait now steps at the spinner own cadence instead, and animations counted in paints land where they would have at full cadence. Any other liveness term, or a stream still arriving within 250 ms, keeps the full cadence."
  - "Idle CPU of a waiting surface was measured at 3 to 6 percent of one core. On the pinned FRESH profile with daily_budget_usd, suppressed host mode, the held wait now reads 2.011 percent against 2.589 before and a slow stream 1.033 against 1.639, a saving of about 0.6 percentage points of a core in both cases. Those figures are suppressed host only: hosted mode, which is what a person runs, is not measured yet."
---

The harness that produced them is in `bench/idle-surface/`. Two cautions travel with it.
Its runs launch `chat --yolo --no-host --max-cost 5`, so every figure describes one process
with no engine daemon. And reproducing them needs `daily_budget_usd` in the profile: without
it a fresh profile stops on the first run settings screen, and the harness cannot tell,
because its poll for the composer logs success after exhausting its attempts.
