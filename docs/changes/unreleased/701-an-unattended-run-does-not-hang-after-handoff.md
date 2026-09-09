---
kind: fixed
title: an unattended one-message run stays for the work it hands over, and says every ending
pr: 701
surface: [chat, docs]
invalidates:
  - "An unattended one-message run (`aforge chat --once \"…\" --yolo` with `--max-hours` or `--max-cost`) ended the instant a turn's work moved onto a task. The door read its own reply's stream and nothing else, so a handover — the write seam, a split at a mark, the ceiling, the turn's wall share — closed the session thirty milliseconds after the node was admitted: the task was cut before its first model call, its checkpoint was left reading `running` with the report `paused — it resumes` for a recovery that a one-message run never gets, its own transcript stopped at two lines, and the wall reader that ends an idle unattended run was shut with the session, so `--max-hours` never fired at all. Measured on `dev@233f729d5` against a scripted endpoint: the stream closed 29 ms after `{\"seam\":\"write\",\"decision\":\"moved\",\"taskId\":1}` and the process exited with the node still `running`. Such a run now stays: the door takes the session's standing subscription to woken replies before it submits, and goes on reading them until nothing it started is still moving and no reply is in flight. A landing wakes the next reply on the same command; the wall reader ends an idle run and stops the work it finds. The waiting is the in-process door's alone — over `--host`/`--at` the session lives in the engine, which keeps the work alive on its own."
  - "`--once` is no longer \"send one message, print the reply, and exit\" full stop. That is still exactly what a plain `--once` does, and what `--once --yolo` with NO ceiling does — both hold a person as principal, have no wall reader and no carry-on — but a run with a budget is a sequence of replies and the flag's row in the manual says so."
  - "The one-message doors said nothing when a run stopped, finished or moved its work. `stopping here · <why>`, `finishing here · what was asked is done`, the write seam's `this is changing more than a quick edit · …` and the wall's `· work was still going, so it was stopped and what it did was kept` are all `EventNotice`, and neither `runChatV3Once` nor `runHostOnce` had an arm for one — so an unattended run could reach its ending in silence. Both doors now write every notice to standard error, beside the `tool:` lines. Standard output still carries the reply and only the reply, so a probe that pipes the answer is unaffected."
  - "`Agent.StillGoing` is the one reading of whether an unattended run has anything left to happen: a reply in flight, a landing note queued that a wake could still answer, or work moving in this session's graph. The session's lock is taken outside the graph's — this package's stated order — and neither is let go of in between, so a task admitted in the gap cannot be read as an idle session. A person's session, a run with no budget and a task's own agent all answer false. A queued landing note counts only while a wake could still start it: past the wall, past the conversation's spending rail, or after the work was stopped, `Agent.wakeLocked` refuses one, and a note nothing will ever answer is not a run still going. Before that clause the door waited out its own `--max-hours` run and had to be killed from outside — `EXIT=124 after 400s` against a one-minute wall."
---

The run this came out of moved its work onto a task and was never heard from
again: no ending, no wall, a task killed a heartbeat after it was started, and a
worktree and a checkpoint left mid-flight in the person's repository. Three of
those are one fact — the door believed its first reply was the whole run — and
the fourth is that nothing it could have said was ever printed.

A run is over when it has been over for a whole settle tick, which is the wall
reader's own discipline for its own reason: a landing hands its lane back a
moment before the note that wakes the next reply is queued, and a door that
believed the first quiet reading would go home over a reply that was already
coming.
