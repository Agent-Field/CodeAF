---
kind: fixed
title: the pull-request gate runs on santos/dev2, the line the work actually lands on
pr: 1362
surface: [build]
invalidates:
  - "CI was read as covering every pull request. It covers the branches a workflow names: `PR gate` named `dev` alone and `Full check` names `staging` and `main`, so a pull request based on `santos/dev2` got the licence check and nothing else, and a push to that line ran nothing at all. The gate now names it too."
  - "`check` being green was read as the line being green. On `santos/dev2` there was no `check` to be green, and the one suite that drives the real binary against a real model is in no workflow at all — see #1361, which found it 7 of 18 subtests red on that line."
---

Two lines in `.github/workflows/ci.yml`. The light gate — build, vet, format,
the laws, the manual, the change entry, and the full suite of every package the
change touched — now answers a pull request into `santos/dev2` and a push onto
it, the same way it has answered `dev` since #372.
