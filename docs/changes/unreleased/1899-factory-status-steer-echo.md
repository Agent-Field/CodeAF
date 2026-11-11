---
kind: fixed
title: Factory check-ins stay out of worker instructions and steering is not echoed in chat
pr: 1899
surface: [engine, chat]
invalidates:
  - "The runner echoed each in-run instruction as another assistant message beginning steer:. It now retains that acknowledgement only in the job audit; the desktop hides older producer-marked echoes without hiding authored messages or renumbering later answers."
  - "Only an exact list of status questions avoided steering. Common natural check-ins now share the read-only status route across send, steer, pending decisions and launch notes; mixed question-and-action requests retain ordinary handling."
---

Status reads leave active work, queued follow-ups, model identity, spending and
pending decisions untouched. Real instructions still reach the active worker
once and remain available to future stages. Historical journals are preserved.
