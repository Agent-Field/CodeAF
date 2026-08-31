---
kind: fixed
title: a background job cannot draw on the chat
pr: 107
surface: [engine]
invalidates:
  - "A job's child could open /dev/tty and paint its output over the running frame — no longer possible. Jobs, watch ticks, standing probes and bash commands run in their own terminal session, with no controlling terminal to open."
---

The top of a running conversation was seen glitching whenever certain background work ran:
jobs were started in a new process group but the surface's own terminal session, so a child
that opened /dev/tty — a CLI that is itself a full screen was the measured case — wrote
straight over the frame. Every such spawn now starts its own session; the open is refused
by the system, everything a job says goes to its log, and the group-kill contract is
unchanged because a session leader leads its own group.
