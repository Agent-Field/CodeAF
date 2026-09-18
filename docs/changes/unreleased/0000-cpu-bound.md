---
kind: added
title: a job subtree is cut when it passes its CPU and process bound
pr: 0000
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
storm — a fork that has not yet accumulated CPU — and is set at 256, seven and a
half times the honest peak, so it is never what separates an honest run from the
spinner storm.

**The ceilings, against those numbers.** A subtree may not sustain more than
three fifths of the machine's cores, and never fewer than twelve: on the 20-core
box the numbers came from that is 12 cores, which clears the honest 8.5 by about
two fifths and cuts the smallest measured storm (16 loops → 16 cores) by a
quarter. The floor of twelve is above the honest peak, so it cannot be what cuts
honest work as measured, and on a machine of twelve cores or fewer the CPU half
is inert — that machine cannot produce the storm's shape at the scale that
mattered. A share with no floor would cut an honest build on a small box, where
the work can legitimately fill every core there is; a fixed number with no share
would be wrong on a large one.

**Sustained, not a spike.** One reading is a spike and honest work has them; the
bound trips only after three consecutive readings over the ceiling, so a
transient never cuts and a storm — over on every reading by construction — is cut
within seconds. A subtree whose usage cannot be read (off Linux, or a group whose
identity no longer matches) is never cut: silence is not pressure.

**The run learns it, and it is not a silent kill.** A subtree that passes its
bound is ended and the run is handed the record `the job subtree was not settled
within its bound`, naming the job and what it was holding — #1183's and #1187's
family, said about the subtree. The kill is a requested death, so the registry's
own ending says nothing and this record is the only account of it.

The reading is /proc, so it is Linux's; no cgroup is required or read, because a
job is a process group and not a cgroup — there is no per-job cgroup to read, and
the process-group walk is the portable handle.
