---
kind: fixed
title: a task waits for the command it started instead of inventing a way to poll it
pr: 574
surface: [engine]
invalidates:
  - "A job completing by event was described as the whole reason a worker never has to poll (#55). It never was, for a task: the exit note queued but nothing was holding the worker open to read it, so the turn loop asked the model for its next step while the command it was waiting for was still running. A task worker now PARKS at its batch boundary until that ending is in front of it (`internal/session/task_job_park.go`), and the polling it used to invent has nothing left to be for."
  - "`still running as job N` meant the same thing everywhere. It does not: in a conversation the turn carries on straight away and the keyboard stays the person's (#55), and in a task the work waits. `internal/manual/chat/what-i-can-do.md` said `The turn carries on straight away rather than waiting` without qualification and no longer does."
  - "Every job in the registry was the same kind of job. There is now `job.owed` — provenance, not state: a foreground call the background-after clock or the command's own timeout took over is one the work is still waiting for, while `bash background: true` is a command the work asked to be FREE of and a person's steer is the person redirecting the work. Only the first holds a worker open."
  - "`jobRegistry.adopt` and `Agent.adoptRunningBashAs` took a `quiet ...bool` variadic. They take an `adoption{quiet, owed}` value, because the road a takeover came down is now two facts and a second bare bool would have said nothing about which was which."
  - "A job's ending was always reduced to its headline on the way to the model (`jobNote`'s `firstLine`). An OWED ending now travels WHOLE — the exit line, the command's last lines and the path to the full log — because the wait was taken so that this ending could be the next thing the work read, and a headline is the reading the wait was taken INSTEAD of. Every other job note is byte-for-byte what it was; the general trim is #573."
  - "`childRun.park`'s rule that the clock is pushed by exactly the parked time was the rule for waiting. It is the rule for waiting on somebody ELSE'S work. A node waiting on a command it started is waiting on its own, so that park pushes no clock, and no single wait may outlast the whole allowance the run was given."
  - "A run that polled a live job and was stopped as circling was a finding about the worker. `internal/session/looped.go` and `childRun.count` are unchanged and still are — but the manufactured polling they were reading is gone, so a task no longer kills a passing test run at 4m35s to report that it went in circles."
---

The reader was right and the question was wrong. A worker's `bash` crossed
`background after`, the call became job 16, and the loop asked the model what to
do next while the eight-minute suite the whole node was waiting on had another
seven minutes to run. There is nothing to do next, so the model made something
up — `sleep 28; tail <log>` — the loop detector read the repetition correctly,
and the node killed a healthy test run.

So the question is not asked. The wait is on the ending being DELIVERED and never
on the process's state, which is the law `task_child_run.go` already states for a
part's report: the release rides in the same locked step as the note's append,
exactly as a person's steered line already does, so there is no instant in which
the ending is queued and the waiter has not been woken for it.
