---
kind: fixed
title: A workstream's collection cannot be deleted by any writer; chat resume skips per-chat database reads
pr: 2303
surface: [app, chat]
invalidates: []
---

An engine that opened the collections database before another engine
migrated it could delete a workstream's collection and with it the
workstream's pages, history, records, go-aheads and plan. The database now
refuses that delete itself. Resuming a chat no longer opens the collections
database once for every saved conversation; it asks only about empty ones.
