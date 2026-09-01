---
kind: changed
title: every part of a division opens on the same world, whoever wrote the parts
pr: 240
surface: [engine]
invalidates:
  - "A part a worker handed out with `divide_work` was given ONLY the per-part `brief` that worker wrote — the parent's brief and the boundary between the parts reached it only where the model doing the splitting restated them once per part. It no longer is: `internal/session/task_divide_compose.go` composes the work being divided and a map of which scopes the other parts own around EVERY part of EVERY division, on both roads, and `divideOnce` is its one call site."
  - "`sketchBrief` (task_divide_sketch.go) was where a part's whole world was written for a harness-drawn division, and the sketch road was the only road that got one. The function is gone: the sketch road writes the scope and nothing else, and gets its context from the same composer `divide_work` does."
  - "`divide_work`'s `brief` field was documented as `THIS PART'S WHOLE WORLD`, which asked the splitting worker to restate the job N times. It is the part's SCOPE — what that one part owns — in the tool schema, `prompts/divide.md`, the division review's own brief and `internal/manual/chat/tasks.md`; the family's context is composed around it and is not the worker's to repeat."
---

What a part was told used to depend on which road put it in the graph, and the
weakest world was handed out on the road a cheap crew actually drives. Splitting a
part's brief by AUTHOR rather than by road settles it: the harness holds the
parent's brief and the settled parts list, so the harness writes them, once per
division and identically for every part; the worker in the material writes the one
thing only it can, which is what each part owns.

The person's ask is untouched and still printed exactly once, by `composeBrief`,
above all of it — a part composed on a parent brief that IS the person's own
sentence carries no ground at all rather than saying it twice.
