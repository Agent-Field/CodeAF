---
kind: fixed
title: Start record triggers for committed source changes
pr: 2948
surface: [app, engine]
invalidates: []
---

A source sync that commits a new record revision can start a matching run using scoped record history. The saved run retains the source event and cause chain across restarts. Freshness notifications without a record revision only advance the outbox cursor.
