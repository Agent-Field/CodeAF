---
kind: changed
title: the six task-state shapes are measured on a real screen, and the tmux suite gets past its front door
pr: 698
surface: [chat, build]
invalidates:
  - "The tmux suite's only tests of a landing that needs a person were `testNestedGate` and `testRefusedLanding`, and both were about the SETTLE DOOR rather than about the words. `TestTaskStatesE2E` in internal/e2e/taskstates_e2e_test.go now measures all three tiers of docs/design/task-states/DESIGN.md end to end: done, your call and the accept, a real branch conflict, an incomplete with its reason, the auto-settle floor, and the rail and roster saying the same word."
  - "ALL SIXTEEN subtests of TestTUIE2E were red on dev@52ff7dde2 and it was one cause, not sixteen. A state root built by newHome opens on the FIRST-RUN SETUP however complete the profile it copied is — the marker that says the setup has been seen is a file in that root — and the flow is several steps, so the one esc some subtests pressed left the step under it and everything typed next went into that step's own box. `start` now presses past it (`rig.skipSetup`); `startFresh` deliberately does not, because a machine that has never run aforge is the subject of its own subtest."
  - "The seeded task fixtures wrote an EMPTY transcript.jsonl, which is not a cheaper fixture but a different screen: the greeting opens over it, on the machine's first conversation it stands through typing (welcome.go), and while it is up the chord keys stand down (stop.go) — so a card's answer letters were refused and `↑` walked the starting points. seedDecidedFamily and seedUndecidedRoot now carry the person's own words, which is what a conversation a task was started from actually holds."
  - "`say(t, …)` now reaches fifteen more names, and internal/e2e/tuiwords_test.go is still the only door: taskDoneGlyph, taskBadGlyph, taskDoneWord, taskOneFileWord, taskMergedFact, taskBranchKeptFact, settleAnswersRow, settleHandRow, settleConflictAnswers, settleConflictNo, taskConflictReason, taskStepsReason, taskAutoDecidingWord, taskTakeItBackWord, taskCardKindGlyph, setupSkipWord. Several are composed at the draw and name their `source` in internal/session, because the WORDS are the engine's and the KEYS are the surface's."
  - "Two subtests are left RED on purpose and each names the issue that owns it: `a_refused_landing_is_incomplete` (#706 — a top-level landing that is your call draws its reason and no answers row, while the same state one fixture over draws all four chips) and `the_rail_and_the_roster_say_the_same_word` (#707 — the column names the work and says no tier word, while the card and the roster both say it in the same frame). Neither is skipped: the suite is where they are visible, and a skip is a defect nobody meets again."
  - "A subtest here may end in a SKIP with its reason on the log rather than a pass. A merge round the engine won and a landing the model settled inside its own turn are both the product behaving, so the conflict and auto-settle shapes wait without failing, assert whichever happened, and say which. A run that provoked neither is skipped out loud and never reported as green."
---

The four packages that pin these words each pin them against a struct they built
themselves, and every one of them was green on the day the four surfaces
disagreed about one landing. This is the same question asked through the door a
person uses: the real binary, in a real terminal, against the pinned model.
