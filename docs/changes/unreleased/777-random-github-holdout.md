---
kind: internal
title: Separate harness tuning from a random GitHub validation set
pr: 777
surface: [build, docs]
invalidates:
  - "The repeatedly used five GitHub issues were the only coding comparison sample. They are now explicitly development data; a separately frozen random sample from other repositories will test generalization."
---

Record the sampling population and seed, reject only uncalibrated environments
before inference, and preserve scored failures. The preparation job runs on
Spark without model requests; scoring follows calibration and client preflight.

A shared grader preserves nested regression directories and requires the same
test identities and outcomes as the calibrated reference, including skips.

The holdout also has a per-trial adapter for the frozen native runners and a
scoped copy of the native mini usage collector. Both remain unvalidated until
the separate Spark preflight; their presence does not mean scoring has begun.

A bounded block scheduler preserves the preregistered arm order and all failed
trials; it refuses duplicate launches and never retries scored work.
