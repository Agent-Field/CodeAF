---
kind: added
title: Factory jobs remember which job they were split from
pr: 2429
surface: [engine, factory]
invalidates:
  - "Was true: a job created from part of another job carried no split lineage. Is true now: factory.job_create accepts split_from, and the new job's reads retain the parent job number."
---

`factory.job_create` accepts optional `split_from`, a positive job number held
by the same Factory and visible to the caller. `factory.capabilities` announces
`create_split_from: true`. The job read, queue and create reply's `current`
return the number when present; other jobs omit it. The new job's own activity
records `split from #N`, and the parent stays unchanged.

Zero and negative numbers are `bad_params` before sending. A missing or hidden
parent is refused before any job or GitHub issue is made, using the existing
422-to-`bad_params` mapping. An older Factory's strict rejection of the field
answers `conflict` with `detail.reason: "factory_too_old"`, without a fallback.
Split lineage is separate from run-as-new lineage, preserving pipeline
stamping, step briefs and provisional-copy cleanup.
