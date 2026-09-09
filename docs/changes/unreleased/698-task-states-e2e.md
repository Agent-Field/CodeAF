---
kind: changed
title: the six task-state shapes are measured on a real screen, not only in unit tables
pr: 698
surface: [chat, build]
invalidates:
  - "The tmux suite's only tests of a landing that needs a person were `testNestedGate` and `testRefusedLanding`, and both were about the SETTLE DOOR rather than about the words. `TestTaskStatesE2E` in internal/e2e/taskstates_e2e_test.go now measures all three tiers of docs/design/task-states/DESIGN.md end to end: done, your call and the accept, a real branch conflict, an incomplete with its reason, the auto-settle floor, and the rail and roster saying the same word."
  - "`say(t, …)` now reaches thirteen more names, and internal/e2e/tuiwords_test.go is still the only door: taskDoneGlyph, taskBadGlyph, taskDoneWord, taskOneFileWord, taskMergedFact, settleAnswersRow, settleHandRow, settleConflictAnswers, settleConflictNo, taskConflictReason, taskStepsReason, taskAutoDecidingWord, taskTakeItBackWord. Several of them are composed at the draw and name their `source` in internal/session, because the WORDS are the engine's and the KEYS are the surface's."
  - "A subtest here may end in a SKIP with its reason on the log rather than a pass. A merge round the engine won and a landing the model settled inside its own turn are both the product behaving, so the conflict and auto-settle shapes wait without failing, assert whichever happened, and say which. A run that provoked neither is skipped out loud and never reported as green."
---

The four packages that pin these words each pin them against a struct they built
themselves, and every one of them was green on the day the four surfaces
disagreed about one landing. This is the same question asked through the door a
person uses: the real binary, in a real terminal, against the pinned model.
