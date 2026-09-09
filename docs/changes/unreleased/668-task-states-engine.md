---
kind: changed
title: a task row answers one question in one word, and no decision stays with the model past a turn
pr: 668
surface: [engine, chat]
invalidates:
  - "A task row's word was the surface's own, and there were four sets of them. Every word now comes from `internal/session`'s `TaskStatus.Word`/`RowWord()` over `TaskTier` (`moving` · `over` · `your-call`); nothing outside that package computes a tier."
  - "`awaiting review`, `unverified`, `needs your look`, `failed` and `delivery needs attention` were person-facing words. They are deleted. The words are `queued`, `waiting on …`, `auto-starts in …`, `working`, `finishing`, `done`, `stopped`, `incomplete` and `your call`."
  - "The landing note the model reads opened `task 7 finished:`, `task 7 failed:`, `task 7 needs your look:` or the halted ending itself (`task 7 lost the connection:`). It now opens `task 7 done:` / `stopped:` / `incomplete:` / `your call:`, with the reason after the title — `task 7 incomplete: Port the parser · ran out of steps`."
  - "There was no single place the incomplete reasons were spelled, so each surface wrote its own. `TaskReasonOf(ending, report)` is that place, and its table is: lost the connection, the model provider refused it, went in circles, was blocked by another task, ran out of steps, would not write its notes down, its brief went stale, would not take a step it was asked to, the check found gaps, a fault."
  - "`taskNote`'s `haltedVerb` and `landingTruth` are gone. A landing's word is read from `ProjectTask(notice.StatusFacts())` like every other reading of the same node."
  - "A decision handed to the model by `task.settle = auto` or by `HandUnverifiedToModel` stayed with the model for ever. It comes back to the person when that model's turn ends, published as an ordinary `EventTaskUpdate` carrying `TaskNotice.Decider`; `Agent.TakeBackDecision(id)` is the same move pressed early."
  - "`task.settle = auto` was read as covering every landing nobody could check. A CONFLICTED landing is never handed to the model — it cannot merge by decree — and its note refuses it the merge in place of the settle clause."
  - "The files that clashed on a refused merge existed only inside the report's prose. `taskTree.comeHome` answers them as a list, `landHome` keeps them on the node, and they ride `TaskNotice.Conflicts` — so a row can read `conflicts with your branch: parser.go, parser_test.go`."
  - "The recovery line said `2 done · 1 failed · 1 unverified`, and the `tasks` tool printed the engine's raw state on every row. Both wear the tier words now."
---

Every task row, card, rail line and roster entry answers one question before it
says anything else: do I need to do anything. Three tiers, one word each,
computed once in `internal/session` and read by every surface, because four
surfaces each working the answer out for themselves is four surfaces that
disagree — and they did, in the machinery's own vocabulary.
`docs/design/task-states/DESIGN.md` is the ruling this builds to.
