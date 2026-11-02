---
kind: added
title: A factory can run edited steps at once from its own copy of a repository's pipeline file
pr: 2013
surface: [engine, docs]
invalidates:
  - "A repository's steps were only ever main's `.codeaf/factory.yaml`, so an edit made in the app ran only once its pull request merged. A factory can now keep its own copy of that file (`PUT /v1/repos/{owner}/{repo}/pipeline/factory-copy`, `factory.pipeline_copy_set`), and the jobs it starts afterwards run the copy instead of main's file. This is an owner decision of 2026-10-28 that knowingly departs from \"team law lives in the repository, read from the main branch\"; `factory.RecipeSource` records who reads what."
  - "`factory.pipeline_source` answered main's file and nothing else. Behind the new feature `factory.pipeline_copy` it answers the factory's copy when one is kept (its text and parse, in state `file`, with `factory_copy` naming who saved it, when, and whether main's file is `missing` or `differs`), plus `copy_version` and `can.use_on_factory`. `base_sha` is still main's commit, which a pull request pins."
  - "A job's rules and a plan's reshaping were bounded by whatever the runner's own machine read of main. A job's pipeline file is now decided once, at its first start (or intake's stamp of its steps, if that comes first), and kept for its life: the factory's copy it then carries (`pipeline_copy` on the job), or main's file, recorded as `pipeline_decided` with no `pipeline_copy`. A runner anywhere reads its rules from that, never its own machine's store."
  - "Every bound on one job was its repository's read. Is true now: one resolver (`pipeline.JobFile`) bounds a job by the file it was decided on (the copy it carries, else main's file alone, never a copy saved later), and only a job never started by the factory's read now (its copy, else main's file, else the built-in steps). It serves step edits (HTTP, the manager's, the local seam's and the item card), the manager's brief, the runner's rules and plan bounds, a post's policy check, a start's ask-first and never rules and budget, and the merge settings pinned at approval."
  - "A job's `pipeline_copy` on the queue and on a job read carries `repo`, `by` and `at` only; the file's `text` is sent only to the runner that pulls the job. `pipeline_decided` crosses on both."
  - "`GET /v1/repos/{owner}/{repo}/pipeline/edit-source` refusals carried no word of a copy. Its failure body now carries `factory_copy` (`by`, `at`; no `text`, no `main`), `copy_version` and `can` whenever the factory keeps a copy, and `factory.pipeline_source` refusals carry a `FactoryPipelineSourceRefusal` detail (`factory`, `repo`, `factory_copy` {`by`, `at`, `main` only when main's file was read}, `copy_version`, `can`), so a copy whose file cannot be shown can still be stopped. `FactoryPipelineCopy.main` is omitted only there."
---

The copy is the very file a pull request of the same edit would write: both
routes check a sent file with the one function (`pipelineFileFor`) and keep
the factory's own encoding. Precedence is the factory's copy, else main's
file, else the built-in steps; the factory's own rules and protected paths
still apply on top. Saving or stopping a copy changes only jobs not started
yet.

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
- A job's file is decided by the first door that starts it or stamps its
  steps (a person's start, a launch, intake's stamp, a new job's chosen
  steps) and never again: a later start keeps it, and its record says nothing
  new. Bring back (`forgetRun`) forgets it with the steps, and the next start
  decides afresh.
- A worker's runner and a job a remote factory runs here read no store copy:
  the manager's brief and the runner's bounds read the file the job carries.
- A copy that spells `kind`, `when`, `effort` or `fanout` is saved only when
  the factory attested, to the same caller, the file it was edited from at the
  commit it serves now, read again before the save, as `factory.pipeline_pr`
  requires.
- A factory that predates the copy route (a 404 or 405 without the phase
  marker) is said as `This factory doesn't support that yet; nothing was
  changed. Update the factory to use it.`, not as a repository it cannot see.
- Stopping a copy the factory does not keep writes nothing and answers the
  version read; the engine takes that as the stop done.
- Main's file is compared before the personal file is merged, so a problem in
  the personal file does not keep a merged copy standing.
- A copy file the factory cannot read refuses starts, like its own rules.
