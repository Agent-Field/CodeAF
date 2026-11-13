# Branches

## Contribution branches

`dev` is the default branch and integration trunk. Start short-lived `feat/`,
`fix/`, `docs/` or `chore/` branches from it and target pull requests at `dev`.
Pull requests merge by squash; delete branches after merging. Stage explicit
paths so unrelated edits do not enter a commit.

## Promotion branches

`staging` points to tested `dev` history. `main` points to a release commit
already on `staging`. Both move by fast-forward, never by merge commits.
Feature work reaches `dev` through pull requests; direct pushes and force-pushes
to these three branches are prohibited except for the deliberate promotion
fast-forwards described in [promotion.md](promotion.md).

Pushes to `dev` and `staging` publish their channel builds. A push to `main`
publishes a release candidate; dispatching `Release` on `main` cuts stable.
Release guards require staging history to be on dev and release history to be
on staging. Inspect `.github/rulesets/` and live branch protection when changing
these rules.
