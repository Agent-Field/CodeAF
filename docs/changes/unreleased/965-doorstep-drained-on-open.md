---
kind: fixed
title: an answer left for a closed conversation is applied as it opens
pr: 965
surface: [engine, chat]
invalidates:
  - "An answer home left on another conversation's doorstep rode that session's presence heartbeat alone (`Agent.drainAnswers`), which is the right beat for a window answering one that is RUNNING. A window shut for the night keeps nothing beating, so the answer sat in `answers.jsonl` until somebody happened to open the conversation — and even then until the new process's first five-second tick. From home the row said `answered · waiting for it to pick that up` for as long as a person cared to look, which was home saying it had answered and home being wrong."
  - "The constructor now drains the doorstep itself, after `recoverTasks` has put back the graph a landing answers to and before `startPresence` starts the heartbeat that would race it. The answer goes through the same one door as every other (`Agent.ResolveQuestion`), so a landing answered from home while its conversation was closed is already settled when the window opens — before its first turn — rather than a question somebody answered yesterday still asking."
---

The person-facing shape does not change: home still leaves the answer on the other
window's doorstep and still says `answered · waiting for it to pick that up` until the
owning conversation applies it. What changed is when that is. A closed conversation used
to have no clock at all; opening it is now the act that picks the answer up, so a landing
answered from home is settled the moment its conversation next runs.
