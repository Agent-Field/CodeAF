---
kind: fixed
title: the first launch of the day no longer waits on the model list before it draws
pr: 1724
surface: [chat, engine]
invalidates:
  - "A model catalog older than 24 hours was fetched again before a lazily loaded catalog answered anything, so the first launch of each day (and any launch after a day away) drew nothing until GET /models came back or hit its fifteen-second ceiling: about ten seconds on an ordinary connection, close to a minute where DNS was failing. A lazy catalog now answers from the day-old cache at once and fetches the new list in the background; the new list is what the next launch reads. Only a machine with no cache at all still waits for the first fetch."
  - "Restarting the machine was taken to be the fix for the v0.5.0 slow start on 2026-10-01. It was not: the restart coincided with a catalog fetched minutes earlier, and the stall would have come back the next day."
---

Opening a conversation asks the catalog several questions through the door that
waits — the agent's tool belt needs to know which media models exist — and every
one of them waited on the same fetch. `internal/catalog`'s LoadLazy already
served the old cache when that fetch failed; it now serves it before the fetch
rather than after it. Eager `Load` (`codeaf models`) and `/model`'s ctrl+r refresh
are unchanged.
