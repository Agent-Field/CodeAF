---
kind: fixed
title: the soft memory limit now takes the cgroup bound, not just physical memory
pr: 1162
surface: [chat]
invalidates:
  - "The soft GOMEMLIMIT a surface sets was half of the machine's PHYSICAL memory. It is now half of the SMALLEST finite bound the machine and the process's cgroup give, so inside a container the limit follows the container rather than the host."
  - "The tuner's comment argued a cgroup limit adds a second kernel interface for a bound that is only ever tighter and 'fails to nothing in a container with no limit'. The cgroup is read now, and a hierarchy with no limit — no files, `max`, the v1 sentinel — is a zero that simply does not participate, so physical memory remains the fallback."
---

`surfaceMemoryLimit` took half of physical memory, and inside a container
physical memory is the HOST's: the limit landed far above what the process could
actually use, never bound, and the kernel OOM-killed instead of the collector
working — the exact failure setting `GOMEMLIMIT` is meant to avoid. The
derivation now takes the minimum of every finite bound it can read: physical
memory, the cgroup v2 `memory.max` for the process, and the cgroup v1
`memory.limit_in_bytes`. A bound that is absent, unreadable, the literal `max`,
non-numeric, or the cgroup v1 sentinel is not a bound and is not counted; if none
is readable the surface behaves exactly as before. The 512 MiB floor is
unchanged and still a refusal.

THE WHOLE PATH IS WALKED, NOT JUST THE LEAF. A parent slice can be tighter than
the process's own cgroup — a container often leaves the leaf at `max` while a
slice above it is bounded — so the reader takes the minimum over the leaf and
every ancestor up to the hierarchy root. Walking can only tighten the answer,
never loosen it, and costs a few extra stats once at startup. On this machine
every level reads `max`, so the walk finds no cgroup bound and the limit is
exactly what it was.

`hostCgroupMemoryLimit` is the reader seam, the same shape as `hostTotalMemory`,
so a test stands a bound in front of the tuner without owning a cgroup; the
reader itself takes the cgroup root and the `/proc/self/cgroup` path, so the unit
tests run against a `t.TempDir()` and a written stand-in and never touch the real
`/sys/fs/cgroup`.
