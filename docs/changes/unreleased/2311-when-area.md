---
kind: fixed
title: Factory file conditions are decided when their step comes up
pr: 2311
surface: [factory]
invalidates:
  - "touches auth and has ui relied only on Triage.Area, which production reads did not populate, so these steps could never run."
  - "A file condition could be omitted while compiling the plan before the write step supplied its files."
---

`when: touches auth` and `when: has ui` use the change's files, then known
pull request files, then an explicitly set triage area. They match whole path
segments or segment names without extensions, ignoring case. Auth includes
auth, billing and crypto; UI includes tui, pages, render and ui.

The runner keeps these conditional steps pending until their turn. With neither
files nor area known, they run conservatively, including before write. Known
nonmatching files end the step as `skipped`, with `SkipReason` (`skip_reason`
on client steps and worker reports) and the job log. Skips advance the run and
survive resume but never count as done work, passed checks or proof. A rerun
re-reads the files and decides again. Hello announces `factory.step_skipped`.
Older desk readers already recognize `skipped` as idle rather than done;
older services reject unknown report states instead of accepting them as passes.
`thin`, `large`, `always` and jobs without a condition keep their behavior.


Changed state and evidence sites (relative to `internal/factory/`):

- `types.go` (`PhaseState`, `Phase`), `report.go` (`StageReport`),
  `api/wire.go` (`Step`), `api/workers.go` (`StepReport`, `WorkerJob`,
  `ToWorkerReport`, `FromRunReport`), and `api/codec.go` (`runStep`, `toPhase`)
  carry `skipped` and `skip_reason`.
- `run/loop.go` (`nextPhase`, `approve`, `runPhase`, `revive`,
  `sendBackStored`), `run/inbox.go` (`askingStep`), `recovery.go`
  (`PlanStepRecovery`) and `giveback.go` (`CheckAgainFrom`) handle progress,
  resume, recovery and rechecking files. `run/diff.go` (`stepsMoved`) and
  `run/report.go` (`reportStep`, `skipPhase`, `setPhaseState`) replicate skips
  and discard stale counts, claims and cached results.
- `pr_body.go` (`PublishBody`), `brief.go` (`BriefFacts`),
  `brief_writer.go` (`stepInputs`, `briefEvidenceOf`, `phaseOf`) and
  `run/policy.go` (`checksGreen`, `skippedCheck`) exclude skipped evidence.
  `cmd/codeaf/factory_verification_checkpoint.go` (`inheritFactoryChecks`)
  excludes skipped source journals; `internal/appwire/server.go` announces
  `FeatureFactoryStepSkipped` from `factory_step_skipped.go`.

The remaining done-only readers deliberately stay done-only: plan declarations,
previous-work targets, manual approval authority, completed-step lists, brief
counts and retained measured-result recovery. Factory setup and ordinary task
`done` strings describe separate state machines. The desk's existing step glyph
maps `skipped` to idle, its summary counts only `done`, and its checks list reads
only supplied test counts. Engines supply no skipped test counts; clients can use
the new feature word to show the reason directly.
