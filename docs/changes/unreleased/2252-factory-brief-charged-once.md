---
kind: fixed
title: Job briefs charge each model call once
pr: 2252
surface: [engine, app]
invalidates:
  - "Saving a brief added its cost again after the session had already charged the job."
---

The job header now includes a brief's provider cost once, matching Spending,
including rejected drafts, dropped claims and failed saves. The session keeps
booking the charge even when another computer runs the job. Saving a version
only records its cost for the existing automatic brief guard; the guard now
compares against the corrected job total.
