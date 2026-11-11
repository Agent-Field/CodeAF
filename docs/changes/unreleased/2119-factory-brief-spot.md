---
kind: added
title: A brief's where-to-look spot opens on its own hunks with factory.job_brief_spot
pr: 2119
surface: [engine]
invalidates:
  - "Was true: a brief's spots named a file and symbol but nothing could show their lines. Is true now: spots are numbered as a version is kept and may carry the writer's notes on their hunks; GET /v1/jobs/{id}/brief/spots/{spot} and, behind the hello feature `factory.job_brief_spot`, `factory.job_brief_spot {factory, job, spot}` answer the hunks of the spot's file in the job's change (narrowed to its symbol when a hunk names it) with the notes as annotations after their hunks."
---
