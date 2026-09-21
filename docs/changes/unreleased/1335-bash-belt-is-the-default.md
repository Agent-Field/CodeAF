---
kind: changed
title: the worker harness is the belt a task runs on, and the switch is now the way out
pr: 1335
surface: [engine, chat]
invalidates:
  - "`CODEAF_TASK_BELT=bash` turned the worker harness on and every machine without it ran the older node belt. The harness is the default now: a `/task` and a `codeaf do` take the run road with nothing set, and the variable only turns it off."
  - "An empty `CODEAF_TASK_BELT` was the same as an unset one and both meant the older belt. An empty value is still the same as unset, and both now mean the harness; the only words that reach the older belt are `node`, `legacy` and `off`."
---
The switch kept its name, its one reader and the sense of every caller. What
changed is the answer it gives when nobody has spoken: `bashBeltAsked` was a
match against `bash` and is now a lookup in a short closed list of words that
mean the older road. Both engines are still in one binary, and every site that
reads the switch reads it exactly as it did.

The list is closed on purpose. An unrecognised value leaves a person on the
harness rather than moving them off it, because a typo in an environment
variable must not be able to change which engine does the work, and the failure
it would cause is silent: the run simply comes out on a road nobody chose. The
empty string is not on the list either, so a variable that is exported and blank
reads the same as one that was never set.

Two refusals that named the old sense say the new one. `NewBeltWorker` and
`LandRunTree` both answered "CODEAF_TASK_BELT is not bash", which described the
way in; they now say it names the node belt, which describes the way out.

The manual pages that told a person the harness was not the default are rewritten
to tell them it is, and to name the three words that turn it off. The eight tests
that spelled their explicit-off as an empty string spell it `node`.

The evidence this lands against is worth writing down, because it does not argue
for the change on its own. `docs/design/bash-task-loop/REPORT.md` measured one
binary over eighteen tasks and put the older belt at seventeen and the bash belt
at thirteen. That comparison predates every wave of the worker harness that
followed it and does not describe the road this switch now chooses, but no run
since has replaced it. The default moved on the owner's instruction, not on a
measurement, and the measurement that would settle it has not been made.
