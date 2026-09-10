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
