---
kind: fixed
title: a factory write step that changed nothing is not done
pr: 2158
surface: [engine]
invalidates:
  - "Was true: a factory `write` step passed on its own `stage_result` done, even when the job's checkout had no change, so test and review ran on an untouched tree and the empty change surfaced only at publish. Is true now: a `write` round that reports done on a checkout with no change from the run's base does not pass; it reads `the checkout has no change from where the run began`, goes round again, and out of rounds asks `write did not finish: the checkout has no change from where the run began · try again, or stop the job?`. Other steps may still finish with no change."
---
