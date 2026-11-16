---
kind: changed
title: moving a chat back to a computer that held it takes about a second instead of ten, however big its folder
pr: 2664
surface: [engine, app, chat]
invalidates:
  - "Was true: every take derived an engine with its own empty clean-file ledger, so the secret screen before an in-place restore and in the dirty check re-read every byte of the folder (7 s of a 9.8 s take-back at 600 MB). Is true now: every engine a process derives (cellstore.EngineFor) shares one ledger bounded at 100,000 files, honoured only while a file's identity, mode, size and mtime are unchanged."
  - "Was true: a one-shot engine process (cellstore.Spawn) opened the store as it found it, and behind a daemon's deferred tail it re-verified the whole pack on every open. Is true now: Spawn.Do first asks a daemon that is already answering to sync that store; it never starts one, and a failed sync only costs the old walk."
  - "Was believed: the slow take-back was the 5 s timers (releaseWithin, the sync and batch intervals, TailQuiet, holdsLeaseWithin, startWait). Measured: none of them is waited on during a take; docs/design/app-v3/MOVE-TIMING.md has the per-phase timeline."
---

On a loopback rig with the field's shape (600 MB folder, engine daemon, shipped
cadence) a take-back went from 9.8 s to 0.8–1.2 s and a take from an offline
holder from 13.0 s to 2.6–3.6 s; a cold take is unchanged and bounded by the
bytes it moves. `TestMoveTimelineRig` (on request) holds PERF.md's ceilings.
