---
kind: added
title: The app engine has one replayable typed event stream
pr: 1800
surface: [remote, docs]
invalidates:
  - "The app transport served only health and shutdown. GET /app/v1/events now serves hello, typed events, replay, reset and keepalive behind the same bearer guard."
  - "The app API draft used bare e_<n> cursors. Cursors now include the launch epoch so an earlier engine's event id cannot match a new launch's sequence."
---

A3 ports the desktop bridge's pull-based feed into `internal/appapi` with a
4096-record circular replay ring and no per-window queues. `Last-Event-ID`
replays strictly later retained records; unknown, expired and prior-epoch ids
receive one reset before live delivery resumes. Repeated agent filters apply
to replay and live frames; turn deltas require an explicitly named agent.
The stream sends keepalive comments every 20 seconds, ends on cancellation
or shutdown and bounds blocked writes to 10 seconds. Resource lanes publish
typed JSON facts through the hub; this transport starts no resource producers.
