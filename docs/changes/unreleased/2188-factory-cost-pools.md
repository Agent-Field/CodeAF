---
kind: added
title: Every finished job counts toward a job's cost, and New job shows an estimate before the job exists
pr: 2188
surface: [engine]
invalidates:
  - "A job's measured cost came only from jobs with a read, so a job nobody read had no figure. Now a job with no read, or too few similar read jobs, is measured on the latest finished jobs of its kind (basis kind, shown as 'not read'); a read match still comes first."
  - "Calibration was one record over every range. It is now kept and widened per basis (read, kind)."
---

`factory.estimate_new {factory, repo, kind}` answers what a job of that kind
in that repository is likely to cost before it exists, from the same kind
pool, with no figure below five finished jobs. No model is asked.
