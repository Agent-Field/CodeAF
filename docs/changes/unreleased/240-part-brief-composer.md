---
kind: changed
title: every part of a division opens on the same world, whoever wrote the parts
pr: 240
surface: [engine]
invalidates:
  - "A part a worker handed out with `divide_work` was given ONLY the per-part `brief` that worker wrote — the parent's brief and the boundary between the parts reached it only where the model doing the splitting restated them once per part. It no longer is: `internal/session/task_divide_compose.go` composes the work being divided and a map of which scopes the other parts own around EVERY part of EVERY division, on both roads, and `divideOnce` is its one call site. What it composes on is what a part INHERITS — the parent's admitted brief AND the reports of the work before it (`TaskGraph.inheritedLocked`, split out of `briefLocked`) — and never the standing orders, which the frontier still appends to each part in its own right."
  - "`sketchBrief` (task_divide_sketch.go) was where a part's whole world was written for a harness-drawn division, and the sketch road was the only road that got one. The function is gone: the sketch road writes the scope and nothing else, and gets its context from the same composer `divide_work` does."
  - "prompts/system.md taught five rules the tool block already carries in front of every request — `bash`'s waiting and its handoff to a job, the jobs law, `propose_task` as a contract in three parts, what a `tasks` row carries, and that steering never moves the brief. Each is stated once now, in the description of the tool it is about, and the prompt keeps only the clauses no description says. The fixed prefix was 758 bytes OVER `fixedPrefixBudget` from #134 (97724633) until this; it is 47,691 under the same untouched 48,000."
  - "`divide_work`'s `brief` field was documented as `THIS PART'S WHOLE WORLD`, which asked the splitting worker to restate the job N times. It is the part's SCOPE — what that one part owns — in the tool schema, `prompts/divide.md`, the division review's own brief and `internal/manual/chat/tasks.md`; the family's context is composed around it and is not the worker's to repeat."
---

What a part was told used to depend on which road put it in the graph, and the
weakest world was handed out on the road a cheap crew actually drives. Splitting a
part's brief by AUTHOR rather than by road settles it: the harness holds the
parent's brief and the settled parts list, so the harness writes them, once per
division and identically for every part; the worker in the material writes the one
thing only it can, which is what each part owns.

A part's whole brief is bounded by `taskShapeBriefLimit` — one number for the
three sections, fitted once per division against the widest boundary and the
longest scope the parts present, so the ground gives way before the boundary or
the scope ever does.

The person's ask is untouched and still printed exactly once, by `composeBrief`,
above all of it — a part composed on a parent brief that IS the person's own
sentence carries no ground at all rather than saying it twice.
