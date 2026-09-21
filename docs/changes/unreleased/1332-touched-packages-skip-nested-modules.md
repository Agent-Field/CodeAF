---
kind: fixed
title: a changed directory in a nested module is not a package of the root module
pr: 1332
surface: [build]
invalidates:
  - "The `touched packages` job derived its test set as every directory holding a changed `.go` file, with no test for which module that directory belongs to. A change that edits a bench fixture under `bench/bashloop/fixtures/` fed `go test` a directory under its own `go.mod`, which answers `main module (github.com/Agent-Field/codeaf) does not contain package …` and failed the job as a setup error while every real package passed. The job now walks up from each directory to the first `go.mod` and keeps the directory only when that `go.mod` is the root's."
  - "`.github/workflows/ci.yml` and the Makefile's `test-touched` target derived the touched set separately, and only one of them was ever fixed. They now carry the same walk, so `make pr-ready` predicts the gate again."
---

The bench fixtures are small programs the task door edits during a bench run, and
each carries a `go.mod` of its own so a worker can build and test it in place.
That makes them modules, not packages of this one, and `go test ./that/dir` from
the root has always refused them. Nothing had touched one in the same change as a
Go file before, so the gap was latent until a branch that edits both.
