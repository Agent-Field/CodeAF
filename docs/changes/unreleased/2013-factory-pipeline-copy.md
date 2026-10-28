---
kind: added
title: A factory can run edited steps at once from its own copy of a repository's pipeline file
pr: 2013
surface: [engine, docs]
invalidates:
  - "A repository's steps were only ever main's `.codeaf/factory.yaml`, so an edit made in the app ran only once its pull request merged. A factory can now keep its own copy of that file (`PUT /v1/repos/{owner}/{repo}/pipeline/factory-copy`, `factory.pipeline_copy_set`), and the jobs it starts afterwards run the copy instead of main's file. This is an owner decision of 2026-10-28 that knowingly departs from \"team law lives in the repository, read from the main branch\"; `factory.RecipeSource` records who reads what."
  - "`factory.pipeline_source` answered main's file and nothing else. Behind the new feature `factory.pipeline_copy` it answers the factory's copy when one is kept (its text and parse, in state `file`, with `factory_copy` naming who saved it, when, and whether main's file is `missing` or `differs`), plus `copy_version` and `can.use_on_factory`. `base_sha` is still main's commit, which a pull request pins."
  - "A job's rules and a plan's reshaping were bounded by whatever the runner's own machine read of main. A job now carries the copy it was queued under (`pipeline_copy` on the job), and a runner anywhere reads its rules from that."
  - "Every bound on one job was its repository's read. Is true now: one resolver (`pipeline.JobFile`) bounds a job by the copy it carries, else the factory's read now (its copy, else main's file, else the built-in steps), for step edits (HTTP and the manager's), the item card, the manager's brief, the runner's rules and plan bounds, and a post's policy check."
  - "A job's `pipeline_copy` on the queue and on a job read carries `repo`, `by` and `at` only; the file's `text` is sent only to the runner that pulls the job."
---

The copy is the very file a pull request of the same edit would write: both
routes check a sent file with the one function (`pipelineFileFor`) and keep
the factory's own encoding. Precedence is the factory's copy, else main's
file, else the built-in steps; the factory's own rules and protected paths
still apply on top. A job already running keeps its steps.

- Only the factory's operator may save or stop a copy, as with the factory's
  own rules (`can.use_on_factory`); on a solo factory that is its one person.
  One route serves solo and team factories alike.
- Both writes require `expect.version` (the slot's version, from 1, counting
  every save and stop); a moved slot is a 409 with the phase marker, and
  nothing is written. Every copy names who saved it and when.
- The copy clears itself on any read that finds main's file is the same file
  (same bytes once encoded), and `factory.pipeline_copy_clear` stops it.
- Logs say `recipe from factory copy`; a job's record says
  `Runs this factory's copy of .codeaf/factory.yaml, saved … by @…, not main's`,
  and the pipeline answer's `origin` is `factory copy`.
- Only a job that has not run takes the copy a start reads; one that ran keeps
  the copy (or main's file) it started under, and its record says nothing new.
- Stopping a copy the factory does not keep writes nothing and answers the
  version read; the engine takes that as the stop done.
- Main's file is compared before the personal file is merged, so a problem in
  the personal file does not keep a merged copy standing.
- A copy file the factory cannot read refuses starts, like its own rules.
