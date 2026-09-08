---
kind: changed
title: the touched packages run on every pull request again, and check is green only when they are
pr: 557
surface: [build, docs]
invalidates:
  - "#499 took the `touched packages` job off pull requests (`if: github.event_name != 'pull_request'`), leaving it on pushes to `dev` only. That is reversed: it runs on every pull request and every push, with no `continue-on-error` and with `-count=1`. In the day it was off, #523 merged red on `cmd/aforge` with `check` green, as #437, #439 and #483 had before the job existed."
  - "The job named `check` was the light gate itself. The light steps are now the `light gate` job, and `check` is a rollup that needs `light gate` and `touched packages` and is green only when both are — the same one-spellable-name shape `full tests` and `cross build` use in `ci-full.yml`. The required name in `.github/rulesets/dev.json` is unchanged; every landing script that waits on `check` now waits on the touched packages too."
  - "A change with no `.go` file and no `go.mod`/`go.sum` runs nothing in `touched packages` and is green in about a minute; a change to `go.mod` or `go.sum` runs the whole tree there."
---

There is no branch protection on this repository, so this is as blocking as a
check can be here: a red `check` is what every landing script and every person
reads, and nothing merges over it by convention.
