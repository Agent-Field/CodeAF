---
kind: added
title: a job subtree is cut when it passes its CPU and process bound
pr: 1190
surface: [chat, engine]
invalidates:
  - "A run's only bounds were its dollar cap and, since #1158, its memory. Nothing bounded a job subtree's CPU or its process count, so a model that spawned busy loops as bash jobs (`yes`, `awk 'BEGIN{for(;;){}}'`) spent no tokens and no bound ever fired. A job subtree is now cut when it passes its bound, and the run is told why."
---

A job subtree is the process group #1155 already records at launch with its
leader's identity, plus every process under it. It now has a bound of its own: a
Go-side watch, portable, sampling the subtree's cumulative CPU and its live
process count through the group handle.

**The signal is the CPU rate, and the process count is a second, higher
ceiling.** The measured honest peak — a max-tier verify — was 34 processes and
8.5 cores at once; the storm was 16 busy loops, then 64. Sixteen loops are
*FEWER* processes than the honest peak, so a process ceiling alone cannot tell
them apart, while 16 cores is far more CPU than the honest 8.5. So the CPU rate
is what separates them; the process ceiling is kept only for the orthogonal
storm — a fork that has not yet accumulated CPU — and is set at 128, about four
times the honest peak, so it is never what separates an honest run from the
spinner storm.

**The CPU ceiling is a share of the cores the subtree can ACTUALLY use, not of
the machine's.** The effective cores are the smallest of the machine's logical
CPUs, the cgroup CPU quota, and the scheduler affinity mask — walked from the
process's own cgroup upward, the way the memory bound is read (#1162). A subtree
may not sustain more than three fifths of that, floored at one core. This is the
correction that makes the bound bite where it matters: a job runs held under a
CPU quota — a cell's own subtree is capped at a few cores — and a ceiling drawn
from the machine's raw count (twelve, on the 20-core box) could never trip under
a four-core quota, because a subtree capped at four cores cannot reach twelve.
Drawn from the effective four, the ceiling is 2.4 cores, and the 64-loop storm
that started this — which pegs every core the quota allows — is cut. On an
unconstrained 20-core box the ceiling is still 12, clearing the honest 8.5 and
cutting the 16-loop storm. The floor of one keeps a single-core machine from a
ceiling of zero.

**Sustained, not a spike.** One reading is a spike and honest work has them; the
bound trips only after three consecutive readings over the ceiling, so a
transient never cuts and a storm — over on every reading by construction — is cut
within seconds. A subtree whose usage cannot be read (off Linux, or a group whose
identity no longer matches), or whose effective cores cannot be sized, is never
cut: silence is not pressure.

**The run learns it, and it is not a silent kill.** A subtree that passes its
bound is ended and the run is handed the record `the job subtree was not settled
within its bound`, naming the job and what it was holding — #1183's and #1187's
family, said about the subtree. The kill is a requested death, so the registry's
own ending says nothing and this record is the only account of it.

The subtree's USAGE is read from /proc, so it is Linux's, and no per-job cgroup
is read — a job is a process group, not a cgroup. The CEILING it is judged
against is drawn from the process's own cgroup CPU quota where there is one, a
different question — how many cores the subtree could ever reach — that falls back
to the machine count off Linux.
