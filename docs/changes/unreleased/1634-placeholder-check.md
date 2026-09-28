---
kind: fixed
title: a check naming issue_.go is sent back, and a file nothing makes starts no paid fix
pr: 1634
surface: [engine, docs]
invalidates:
  - "`plandb add --check` and `plandb task set-checks` stored any check as typed, so a fan-out whose shell loop lost its number stored `gofmt -l issue_.go` on every part. A check naming a file whose stem ends in `_` right before the extension, missing on disk, beside numbered files on disk or in the plan (`issue_07.go`, `issue_NN.go`) is now refused with an `error:` sentence that names the numbered files and the part's own example and ends `Nothing was added.` or `Nothing was changed.`; the check is never rewritten."
  - "Every `does not hold:` finding in the review round started a paid `fix:` task. A finding that names a missing file which no task names in its title or work order, and which is a lost-number placeholder or named by two or more tasks' checks, now starts none; the task keeps the finding and a second note, `No fix was started: the check names <file>, a file nothing in this run makes`, says why."
  - "#1573 was open with no working fix after #1604's blanket missing-path refusal was taken back. Packages such as `go test ./internal/rank`, globs, variables, existing files and a missing file only its own task's check names are still admitted and still get their fix."
---

A planner that loses the number in a shell loop now hears about it before the check is stored,
and a check that could never hold stops buying repairs that could never make it hold.
