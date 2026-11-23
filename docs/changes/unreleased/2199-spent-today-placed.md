---
kind: fixed
title: the factory's spent-today figure no longer counts a whole older job because it moved today
pr: 2199
surface: [engine]
invalidates:
  - >-
    Was true: factory.queue since.spent_today added a job's whole run spend when
    the job changed, started or ended today. Is true now: it counts spend placed
    today by the same rule as the spend-by-day reads, so a job that began on an
    earlier day counts nothing today and the figure can read low, not high. The
    daily rail keeps its own figure.
---
