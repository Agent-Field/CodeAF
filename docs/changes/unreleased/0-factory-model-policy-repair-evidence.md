---
kind: fixed
title: Factory model-policy refusals keep work running and repair completion requires current measured checks
pr: 0
surface: [engine, chat]
invalidates:
  - "Requesting Automatic under a one-model launch could stop the running factory stage. The unavailable request now records a notice and leaves execution and valid model changes running."
  - "Effective Fixed policy under a launch constraint could be saved as an explicit preference. Snapshots now distinguish effective mode, optional explicit chosen policy and the current launch lock reason."
  - "A repair could report fixed after npm audit failed behind successful output presentation. Supported audit, test, lint and build exits are measured and bound to current checkout contents before fixed is accepted."
---

The desktop model menu can explain and disable Automatic while a launch lock is
in force. Explicit Fixed and Automatic choices remain distinct from inherited
policy and temporary constraints across save and restore.

Factory repair completion rejects failed, missing, unknown and stale local check
evidence before accepting stage_result. The worker can rerun checks in the same
conversation or report needs_repair for partial work. Check fingerprints cover
HEAD, tracked changes and nonignored untracked contents without changing the real
index. Manager questions and plan edits retain their original routing. Readiness
still requires complete current-head GitHub checks and review; locally passing
checks do not claim the newly pushed PR has passed GitHub CI.
