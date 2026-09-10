---
kind: changed
title: A wide READ is quick tasks and a wide CHANGE is one task — and independent pieces start in one breath
pr: 811
surface: [chat, engine, docs]
invalidates:
  - "The belt's first hand-off bullet sorted work by WIDTH: `WIDE WORK … ONE propose_task with wide set. That is the default road.` It sorts on what happens to the answer now. Work you will READ and carry on with — a survey, a comparison, research, a draft, reading across many files or packages — is `quick_task`, one per independent part in one breath, or one with `items` where the parts share what they learn. Work that must be CHECKED AND LANDED on its own, or must outlive the window, is `propose_task`. The `one task that hands its own parts out` road is a wide CHANGE and never a wide read."
  - "prompts/system.md said `Wide work is one task that hands its own parts out once the material shows the width is real`, with no other road named. It now says that of a wide CHANGE only, and says outright that a wide READ is quick tasks, one per part."
  - "Nothing told the model to weigh the wall clock. A new belt fact does, on the same predicate the two verbs come off: independent pieces are started in one breath rather than one after another, one is kept and begun at once, a running task is never polled or re-read, each landing is folded as it arrives, and the turn ends once nothing independent of the handed-out work is left."
  - "Nothing told the model how big one quick task should be. `quick_task`'s description now says a few files and a few minutes, quoting `taskNoProgress` for the steps that only read before a node is stopped: a big package is several quick tasks of a few files each, and a quick task's own children are smaller still."
  - "prompts/system.md's `WORDS … and so is small work: a few tool calls, one obvious edit, a file read and a verdict` is gone — the judge in `quick_task`'s description makes that routing call, in the place the call is made. PERF.md's debt ledger named it; the `WIDE WORK` bullet's `never split related work` went with it."
  - "The chat manual's *Quick task or a proper task* taught the judge and nothing about width. It now says what decides is what happens to the answer, never how wide the work is, and two new sections beside it — *How big one quick task should be* and *Why several things started at once* — carry the grain and the wall-clock law."
  - "*Should this be a run, or one worker that splits itself* and *When a run is the wrong tool* said wide work of every kind is one task. Both now split the wide change from the wide read, and send the read to quick tasks."
---

The judge between `quick_task` and `propose_task` was right and unreachable: the
belt's own bullet above it still sorted work by width, and width won. Asked for a
read-only survey of four packages, a real model said *"wide survey across four
packages — sizing it before I hand it off"* and proposed a task, buying a
worktree, a check and a landing for four files nothing was going to write. Told
"one quick task each" it made four quick tasks that all landed inside two
minutes, so the machinery was never the problem.

Prompt and description text only; no engine change. The wording is proved
against the real model rather than argued: the choice battery runs the untold
prompts through `bin/aforge` in a terminal and reads the first turn's tool calls.
