---
kind: fixed
title: a task's work log carries its real steps, and a note says who left it
pr: 0
surface: [engine]
invalidates:
  - "Was true: `tasks.get` sent a task's work log as worded lines, every one of them a `notice`, because `AppTaskView` only ever carried the sentences `taskLive` composed and `GetTask` wrapped each in `EntryNotice`. A surface that sorts a log by `kind == \"tool\"` — which is how a conversation's log is read — found nothing in a task's, so a task that ran sixty-two tests over seven steps drew an empty work log and a footer that said nothing about it. Is true now: `AppTaskView` carries `Steps []AppTaskStep`, a typed record projected from whichever of the three genuine records answered (the live ring, the node's journal, a handed-over run's trajectory), and `tasks.get` sends one tool `Entry` per step with its call's own facts — tool name, action family, command, start, end, duration, exit code, outcome, output tail. Each fact appears ONLY where it was recorded: no `ended_at` without a recorded end, no `exit_code` without a reported exit, no `ok` for a step still running or one a killed session left unsettled."
  - "Was true: a step from an older record that kept a command and nothing else about it was indistinguishable from a step the engine understood, because both arrived as the same notice. Is true now: a record with no tool payload stays a quiet notice carrying its own words and is never given a tool kind its record never declared. Classifying one would be asserting a tool on no evidence."
  - "Was true: `appTrajectoryRead` read a handed-over run's trajectory for its `command` alone, dropping the tool, the clock, the exit and the output that the same line already carried. Is true now: `appTrajectorySteps` reads the whole line into a typed step and the worded line is composed back out of those facts, so the sentence and the row cannot disagree."
  - "Was true: `appwire.TaskNote` had no origin field, and `AppTask`'s plan-task fallback read `PlanTaskNote.Body` while discarding its `Person` flag. Every note arrived unlabelled, so an app that read the absence as the person's drew a worker's handoff and its account of the work in their own bubble, as words they never typed — the same report appearing twice on the page, once as the answer and once as a thing they had said. Is true now: a note carries `from` (`you` or `task`) from the engine's own record. AN ABSENT ORIGIN IS UNKNOWN AND NOT YOURS: the field is `omitempty`, so a record that never labelled a note serialises exactly as before and a surface must not attribute it."
  - "Was true: `task.steps` was sent whenever the view's step counter was above zero, which made a task nobody counted and a task that genuinely took no steps the same row. Is true now: `AppTaskView.StepsKnown` says whether any record kept a count, and `steps` is sent when known INCLUDING A KNOWN ZERO and omitted when not. A footer can then drop the step word rather than claim `0 steps` about history it does not have."
---

The typed record is bounded the way the live ring is (the last 200 steps, 12 tail
lines and 2000 characters of text each), and a durable journal or trajectory is
reshaped only when the file itself moves: a size-and-mtime fingerprint caches the
parse across the per-second polls, so opening a task does not re-read a large
journal on every tick. The live ring is preferred over the journal and the two
are never merged, because a restarted task's steps are in both and a merge would
draw them twice.
