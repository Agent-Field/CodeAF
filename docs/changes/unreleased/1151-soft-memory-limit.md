---
kind: added
title: a chat surface now runs under a soft memory limit drawn from the machine
pr: 1151
surface: [chat]
invalidates:
  - "The surface launch raised GOGC to 400 with nothing bounding it. A surface now also sets a soft GOMEMLIMIT of half the machine's physical memory, and carries it to the engine host the way GOMAXPROCS is carried."
---

`tuneForTheSurface` capped the scheduler and raised the heap target but left the
other half of that bargain open: a heap five times the live heap had no ceiling,
so a surface left open for a day could find the ceiling by exhausting the
machine. It now sets `debug.SetMemoryLimit` — a soft limit over all
runtime-managed memory — to half the machine's physical memory, with a 512 MiB
floor, and writes `GOMEMLIMIT` into the environment so the separate
`engine --daemon` inherits it at its own startup. An explicit `GOMEMLIMIT`
decides untouched, exactly as an explicit `GOGC` does, and a machine whose half
would fall under the floor — or whose memory cannot be read — has nothing set at
all, because a limit below the live heap makes the collector thrash and is worse
than the growth it prevents.

The limit is drawn from physical memory rather than a fixed ceiling or a cgroup
limit: a fixed ceiling is wrong on both a small and a large machine at once, and
a cgroup limit adds a second kernel interface that fails to nothing in a
container with no limit. Physical memory over-estimates inside a container, and
an over-estimate is the safe error — it makes the limit loose, never tight.

**What this does not establish.** No measurement was made and none is claimed:
this cell says only that a surface now has a bound and that an explicit setting
still wins. That the bound improves anything — RSS, pause time, anything — is a
separate question a separate cell measures.
