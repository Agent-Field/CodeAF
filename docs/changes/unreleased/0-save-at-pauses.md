---
kind: changed
title: Background work folder saves wait for pauses between commands
pr: 0
surface: [app, chat, engine, docs]
invalidates:
  - "The session previously forced a capture after every tool batch. Pipeline step settles now hold unknown-effect calls until the oldest unsaved completion reaches 30 seconds or a forced settle saves the pending batch."
  - "Head boundaries previously only waited for already scheduled captures. Between steps they now force held calls into a capture; during an open step they retain the WAL completions and only wait for an existing capture."
---

Exact-path-only and transcript-only steps still capture immediately when no
unknown-effect call is pending. Setup calls and synchronous recording keep their
existing behavior. Turn end and other forced settles capture everything pending.
Boundaries never wait on an open step's tool. Displacement between steps preserves
held calls in its kept branch, and cell file rewind refuses an open step. Finished
call records keep the existing WAL recovery path; conversation rewind still
leaves files changed.
