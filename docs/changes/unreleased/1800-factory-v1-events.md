---
kind: added
title: Factory pushes typed notifications and replays recent live events
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "The factory event stream sent only changed ticks without payloads. It now also pushes needs_you, question, approved, taken, publishing, published, publish_failed and machine events with small JSON payloads."
  - "Reconnecting lost earlier events and idle streams had no heartbeat. Last-Event-ID now replays the last 200 typed events from a durable record, and keepalive comments arrive every 20 seconds."
---

Every publishing and Needs you transition is recorded at the store write so
rapid transitions survive filesystem batching. Push batching is bounded at
100ms during continuous writes. Viewer filtering covers live events and replay;
machine events follow heartbeat deadlines and the same roster as /v1/me.
An immediate request for changes waits for the final proof write and retires
the finished run, so a live landed snapshot can start the next round. The existing changed
event and Contract 1 stay available.
