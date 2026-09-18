---
kind: added
title: a job subtree is cut when it passes its CPU or process bound
pr: 1190
surface: [chat, engine]
invalidates:
  - "A run's only bounds were its dollar cap and, since #1158, its memory. Nothing bounded a job subtree's CPU or its process count, so a model that spawned busy loops as bash jobs (`yes`, `awk 'BEGIN{for(;;){}}'`) spent no tokens and no bound ever fired. A job subtree is now cut when it passes its bound, and the run is told why."
---

A job subtree is the process group #1155 already records at launch with its
leader's identity, plus every process under it. It now has a bound of its own: a
Go-side watch, portable, sampling the subtree's cumulative CPU and its live
process count through the group handle. The bound has two halves that apply in
different places.

**The process backstop applies always.** A subtree may not hold more than 128
processes — about four times the measured honest peak of 34 — quota'd or not,
because a fork storm that has not yet accumulated CPU is invisible to a rate and
a cgroup CPU quota does nothing to a process count.

**The CPU rate rule applies only where nothing else caps the process.** When a
cgroup CPU quota or an affinity mask BINDS this process — the cores it may use
are fewer than the machine's — the quota is already the ceiling on what any
subtree under it can burn, and #1187's park bound is what returns a turn wedged
on a job that never ends. A CPU rule there would only cut honest work early:
inside a four-core cell an ordinary `go test` legitimately uses most of its four
cores, and a share of them would end it. So under a binding quota the CPU rule is
OFF and only the process backstop applies. When NOTHING binds — the process may
use the whole machine — the CPU rule is the only thing between a spinner storm
and the box: a subtree may not sustain more than three fifths of the machine's
cores. On the 20-core box the numbers came from that is 12 cores, clearing the
honest peak (8.5) and cutting the storm (16 loops → 16 cores, 64 → 20).

Whether a quota binds is read from the process's own cgroup CPU quota (v2
`cpu.max`, v1 `cpu.cfs_quota_us`/`cpu.cfs_period_us`, walked from the process's
cgroup to the hierarchy root the way the memory bound is read, #1162) and its
scheduler affinity mask; off Linux nothing binds and the machine-cores rule
governs as before.

**Sustained over half a minute, not a spike.** A build burst can pass twelve
cores for several seconds, so the bound trips only after the subtree has been
over on fifteen consecutive readings at a two-second interval — thirty seconds
sustained. A transient never cuts, and a storm — over on every reading by
construction — is cut half a minute in. A subtree whose usage cannot be read
(off Linux, or a group whose identity no longer matches) is never cut: silence is
not pressure.

**The run learns it, and it is not a silent kill.** A subtree that passes its
bound is ended and the run is handed the record `the job subtree was not settled
within its bound`, naming the job and what it was holding — #1183's and #1187's
family, said about the subtree. The kill is a requested death, so the registry's
own ending says nothing and this record is the only account of it.
