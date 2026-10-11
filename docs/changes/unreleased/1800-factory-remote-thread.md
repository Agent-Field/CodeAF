---
kind: fixed
title: Factory manager threads and replies work across machines
pr: 1800
surface: [chat, remote]
invalidates:
  - "The thread route read a transcript on the factory machine and was empty for remote jobs. Workers now replicate bounded manager lines into the job store."
  - "Reply had no service door and returned 501. It now stores a pending reply, delivers it through the holding machine's worker signal, and records delivery."
  - "Answered questions disappeared from the job. The latest 100 now keep the person, text, time and answer or approval event."
---

Idle manager replies run no job steps. Pending replies survive lease expiry and
service restarts, and stay pending while the holding machine is offline. The
additions preserve the existing /v1 fields and Contract 1.
