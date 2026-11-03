---
kind: fixed
title: Workstream plans, pages and records have size limits; agent Spec pages are read by jobs
pr: 2310
surface: [app]
invalidates: []
---

A plan's draft text is at most 80,000 bytes; a plan holds at most 500
milestones, 500 waves and 5,000 items in 4 MiB; a workstream holds at most
5,000 pages (deleted included) and 5,000 records. Past a limit the app gets
`too_large` and nothing changes. Pages the workstream agent creates titled
Spec or Decisions are read by jobs, as pages the person creates are.
