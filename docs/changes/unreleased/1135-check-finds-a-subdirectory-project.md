---
kind: fixed
title: The finished-tree check finds a project cloned into a subdirectory
pr: 1135
surface: [engine]
invalidates:
  - "`internal/verify/photograph.go` took the reading at the workspace root only, so a task whose project was cloned into a subdirectory (awilix under `./repo`, bandit under `./bandit`) discovered no manifest, Makefile or script and the reading came back \"this project declares no way of checking itself\" — no check ran and broken work shipped. The reading is now taken at the immediate subdirectory that declares a check when the root declares none."
---

The reading has to be taken where the project is, and a corpus task's project
is often one directory down from the errand's workspace root. When the root
declares no way of checking itself, `photograph` now looks one level down and
takes the reading at the single immediate subdirectory that does — chosen by its
own files, the same evidence the root is held to, never by its name, and sorted
so two readings of one workspace choose the same directory. The reading is
re-rooted onto that project so the second reading, the journal and the delivery
gate all run the command where the project is. A root that declares its own
check is unchanged.
