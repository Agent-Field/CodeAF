---
kind: fixed
title: Factory file conditions are decided when their step comes up
pr: 0
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
nonmatching files skip the step with the reason in its note and the job log.
`thin`, `large`, `always` and jobs without a condition keep their behavior.
