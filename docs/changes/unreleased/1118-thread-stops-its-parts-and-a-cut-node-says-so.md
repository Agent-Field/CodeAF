---
kind: fixed
title: a design's thread stops its parts before it closes, and a node machinery cut says what cut it
pr: 1118
surface: [chat, engine]
invalidates:
  - "The nursery law was enforced on one road only. `workTaskNode` stopped a worker's parts before closing it, but the design body closed its thread with a bare `child.Close()` and no `stopChildren` — and that thread is not a leaf, so parts it had handed out were cut down with it unmarked, read their own cancel as a process quitting, and landed on the `paused — it resumes` road with their state left running for a recovery that was never coming. The design body now carries the same guard, on every road out."
  - "A node whose context ended without anybody marking it stopped wrote nothing about why: no ending, no cancelling party, no reason, so an interruption a person caused and machinery cutting the work read the same on the record — which is to say, neither read as anything. Such a node now carries `TaskEndingInterrupted` and a report saying the cut came from outside the work. It still does not move the state and still leaves the checkpoint resumable: an interruption is not a finding about the work."
  - "`TaskNotice.Ending` was set only on a node that settled `failed`. It is now also set on a node machinery cut where it stood, which stays running for recovery to resume; every other state still carries none."
---
