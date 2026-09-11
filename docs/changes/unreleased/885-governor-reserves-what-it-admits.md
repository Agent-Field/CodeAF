---
kind: fixed
title: a fan of tasks starts what this machine can carry, not all of it at once
pr: 885
surface: [engine, chat]
invalidates:
  - The admission governor used to be asked ONCE PER SCHEDULING PASS and every ready node
    started against that one reading, so a parent handing out twenty parts started twenty
    agents, worktrees and builds against a machine read before any of them existed. It is
    asked once per ADMISSION now, and every node that starts sets aside a measured
    footprint of memory against task.min_free_mb until a reading shows it.
  - "task.min_free_mb used to be compared with MemAvailable as read. It is compared with
    the projection now: MemAvailable minus the part of the running nodes' footprints the
    reading cannot see yet. Nothing is counted twice — as a node's memory appears in the
    reading, what is set aside for it falls by the same amount."
  - "A task node's expected memory footprint is now a figure this machine gives rather than
    nothing at all: the larger of one core's share of MemTotal and the most memory per
    running node this session has been watched to hold. A quiet machine starts roughly one
    task per core's share of the memory above the floor and holds the rest saying
    machine busy."
  - TaskGraph.machineBusy is gone. The division's receipt reads what the frontier decided
    rather than asking the machine a second time after the parts were admitted, and it now
    says SOME of the parts are waiting, because a fan wider than the machine is half
    started and half held.
  - A queued CHILD row in the roster drew no reason at all, whatever was holding it. A held
    row now draws its reason wherever it sits in a family.
---

Load average is a one-minute decayed figure and a task's own memory arrives with its first
build, so no reading taken at admission can see the burst it is about to let in. The
governor keeps a reservation for the work it has already admitted instead: the frontier
asks it per node, with the nodes that same pass has started counted, and the held ones
start as earlier ones settle or as a reading shows room. Nothing about what any model is
asked changed — only when a node starts.
