---
kind: changed
title: a worker stopped by the write-your-notes rule lands under its own words
pr: 182
surface: [chat, engine]
invalidates:
  - "A task worker stopped by the write-your-notes rule settled with no ending at all, so its rail row fell back to `stopped — branch kept` — the sentence reserved for a person's own stop. It has its own ending now: `TaskEndingNotes`, drawn as `would not write its notes down — branch kept` with the `!` that asks for a steer."
  - "`TaskEnding` had eight values. It has nine, and `haltedVerb` names the new one, so the landing note the conversation is handed reads `task 7 would not write its notes down: <title>` rather than `task 7 failed: <title>`."
  - "The graded record for such a node said `outcome: did not finish`, which is what a run that ran out of steps says. It says `stopped`, the same word a person's own stop earns: both are a decision to end the run taken from outside, and neither is a reading of what the work was worth."
  - "`endingOfClaim` took two arguments and its only evidence was the run's LAST WORDS matched against the loop guard's sentence. It takes three, and the first is a fact: a turn a process rule stopped says so as a value the loop wrote down when it stopped it (`ruleStopWitness`), never as prose anybody has to grep."
  - "A `processRule` answered four questions. It answers five: `ending()` names the `TaskEnding` a worker it stops wears, so a second rule in that registry brings its own row rather than being folded into the notes rule's words."
  - "`how-tasks-run.md` said this stop \"is never written up as it\" — it now has a section of its own saying what the row, the landing note and the graded record each read, and `keys.md`'s `[held]` section says what happens when the stopped turn belongs to a task worker."
---

PR #154 built the rule and left this out on purpose: the landing lives in
`task_run.go`, which other lanes owned that week. Nothing about the enforcement
changes — what changes is that the node it stops stops settling as an ordinary
failure. A worker that would not write its notes down is halted news, not a
finding: the branch is kept and the next move is a person's.
