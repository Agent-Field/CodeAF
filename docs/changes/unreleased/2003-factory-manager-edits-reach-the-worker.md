---
kind: fixed
title: The manager's step edits reach a running job's worker; a fixed pipeline binds it before a run
pr: 2003
surface: [engine, chat, docs]
invalidates:
  - "Was true: the manager's `factory_run` edit wrote the job's steps on the factory alone, while the worker kept running the job from its own copy, so the edited steps never ran and a report that reshaped the pipeline wrote the worker's steps back over the edit. Is true now: the manager's edit goes through the same door as a person's step edit: in one change it rewrites the steps still to come and withdraws the worker, which pulls the job again with the edited steps, and a report from the old copy is refused."
  - "Was true: a pipeline's `adapt: fixed` (or the factory's own adapt word set to fixed) bound edits during a run only, so the manager could still change the steps before the run began. Is true now: under fixed the manager's edit is refused before a run as well, with \"the recipe for issue is fixed; manager may not change the stages\". A person's own edit is not bound by it, and the default (`adapt`) still lets the manager and the plan reshape the steps."
---
