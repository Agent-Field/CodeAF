---
kind: added
title: Factory /v1 lists machine presence and the unassigned toggle
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "`GET /v1/me` always returned an empty machines list and an empty takes_unassigned map. It now lists the caller's own machines and the map saved for that handle."
  - "A job did not say which machine held it, and queue capacity had no machine counts. A live lease is now held_by on the job, and capacity reports machines_online and busy."
---

`PUT /v1/me/takes_unassigned` stores the per-repo pick-up toggle for the
caller's handle. Once that map exists, the worker loop's pulls use it.
Contract stays 1.
