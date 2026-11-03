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
