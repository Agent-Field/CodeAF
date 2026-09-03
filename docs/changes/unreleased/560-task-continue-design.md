---
kind: internal
title: the design for continuing a task from any ending
pr: 560
surface: [docs]
invalidates:
  - >-
    "Nothing on this machine re-runs a task, and the verb is named nowhere" is
    what `internal/tui3/place_tasks.go:1064-1079`, `internal/manual/chat/tasks.md:3609`
    and `TestTheTasksPlaceNeverNamesRunItAgain` all say, and it is the position
    this design argues against. Nothing has changed in the code yet — this pull
    request is the design page alone — but anyone reading those three as settled
    should read `docs/design/task-continue/DESIGN.md` first.
  - >-
    `internal/manual/chat/how-tasks-run.md:2033` already tells a person to say
    `continue task 7` about a halted task, and no such verb exists: the `tasks`
    tool takes `query, limit, id, lines, scope, say, resolve`, and `resolve`'s
    enum is accept, reaudit and refute. The page is a promise the code does not
    keep, and the design settles it in the page's direction rather than the
    surface's.
---

Design only, no code. The page reads the eleven endings a task can have off
`TaskState`, `TaskEnding` and the five landing roads, and says what is durable at
each: nine of the eleven end with the branch, the working copy, the journal and
the findings all on disk and no verb pointed at them. The two exceptions are both
about success — a finished chat task loses its tree to `releaseLanded`, and a
clean headless run loses its record to `keepPrivateStore`.

One mechanism covers all eleven, and it is #544's remainder shape lifted from a
leaf to a task: the original assignment peeled out of whatever wrapper it is in,
the criterion unchanged, one finding added, and the bank the ending left. The
seam is `taskStore.interrupt`, which already performs the whole transformation
for exactly one ending because it has exactly one caller.

The two success endings are the two things the design changes about durability
rather than limits it states: a finished task's tree is not lost but landed, so a
follow-up cuts a fresh working copy from the commit its work landed at; and a
clean headless run keeps its `graph.db` and drops its `graph-scratch/`, swept by
age, so a task id is an address that can still be looked up. The headless shape
is derived against `docs/design/polish/COMMANDS.md` (`ui/polish-v0`, PR #518) —
its nouns make the argument a task id rather than a path, its §4 gives `--dir`,
and its §5 puts the record path on stderr and gives the exit ladder a
continuation reads.

The corpus half is filed separately as issue #562: `how-tasks-run.md:2033`
already tells a person to say `continue task 7`, which is a defect today
independent of whether this design is ever built.
