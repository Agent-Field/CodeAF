---
kind: fixed
title: An idle runner no longer rereads every job ten times a second or leaks sockets to its own factory
pr: 2122
surface: [engine]
invalidates:
  - "An open /v1/workers/pull looked for work every 100 ms, reading every job document each time. It now looks when it opens and when a file under the factory root changes, other than the runner registry and the live record. A lease that only expires is requeued by the next pull, at most one pull wait (30 s) later."
  - "Every factory client opened its own HTTP connection pool. All factory clients in a process now share one pool."
---

A machine with 15 runner slots and 340 Backlog jobs spent 60–150% CPU while idle, and its loopback connections to its own factory listener grew without limit. Both now stay flat: under 1% CPU and one connection per slot.
