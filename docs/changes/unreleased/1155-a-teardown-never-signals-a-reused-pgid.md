---
kind: fixed
title: a job teardown never signals a process group whose pid was reused
pr: 1148
surface: [chat, engine, resident]
invalidates:
  - "A job was torn down by signalling its recorded process-GROUP id with no check that the pgid still belonged to the job: `internal/processgroup`'s `Kill`/`Terminate`/`Alive` all act on `kill(-pid, …)`, which reaches whatever process group currently holds that pgid. Under pid pressure a finished job's number is handed out again, so a settle/close sweeping several job groups could SIGKILL an unrelated, live group — observed twice taking down a tmux server and ~12 running jobs. A group is now recorded at launch with the leader's identity, and every group signal is sent only while that identity still matches the live process; on a mismatch, a reaped child or a free pid nothing is signalled."
  - "The detached-group sweep that runs when a shell exits after backgrounding a child (`terminateDetachedGroup`) read and signalled a bare pgid after `cmd.Wait` had already reaped the leader, so it had nothing to check the group against. On Linux the reaper now holds the shell as a zombie with `waitid(WNOWAIT)` so the sweep runs while the leader's identity is still readable, and the sweep is identity-checked; where there is no wait-without-reap the sweep keeps its old shape rather than leak every detached child."
  - "`StopServiceProcess` killed the whole detached session of a service by its recorded pid alone. It now refuses unless the live process still matches the start time the service was recorded with."
---

A process-group id is not an identity. `kill(-pid, SIG)` reaches whatever
process group holds that pgid now, and pids are recycled — the incident this
fixes was a long-lived codeaf tearing several job groups down at once and
SIGKILLing a pgid that had by then become the DOE's `tmux -L pareto` server,
taking about a dozen running jobs with it. So a group is recorded at launch
with the leader's start-time identity (`/proc/<pid>/stat` field 22 on Linux, `ps
-o lstart=` elsewhere), and `internal/processgroup.Group` gates `Terminate`,
`Kill` and `Alive` behind that check: a signal is sent only while the recorded
identity still matches the live process, and on any doubt — a mismatch, a reaped
child, a free pid — nothing is sent. The safe default is one-sided on purpose: a
missed kill leaks one process, a wrong kill destroys somebody else's work.

`internal/exec/jobs.go` and `internal/session/jobs.go` record the group where
they fork and route every teardown signal through it; `internal/exec/services.go`
checks the recorded start time before stopping a service. The Windows path keeps
its old, ungated `taskkill` behaviour, and non-Linux Unix keeps the old detached
sweep because there is no `waitid(WNOWAIT)` to hold the leader readable.
