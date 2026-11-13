# Promotion and releases

## Promote to staging

`Promote to staging` runs every Friday after a 17:00 America/Toronto cutoff and
promotes the first-parent `dev` commit at the cutoff. To run it by hand, dispatch
it from Actions on `dev`: leave `target` empty for the current dev tip, `cutoff`
for the Friday cutoff commit, or supply a commit already on `dev`. Use `dry_run=true` to
validate without pushing. The workflow preserves branch pointers, runs a fresh
`Full check` on the exact candidate, rechecks ancestry, and fast-forwards
`staging` only after success. It waits for the staging build to publish.

A missing promotion token, failed check, diverged branches or refused push leaves
the pointer unchanged. If the pointer moved but publication failed, inspect the
linked `Release` run before retrying. Run summaries report the outcome. Never
force a promotion.

For a manual promotion, fetch the pointers, select a dev commit, confirm ancestry
and dispatch Full check for that commit:

```sh
git fetch origin
SHA=$(git rev-parse origin/dev)
git log --oneline origin/staging..$SHA
git merge-base --is-ancestor $SHA origin/dev
gh workflow run ci-full.yml --ref dev -f ref=$SHA
```

Wait for that run to conclude successfully on `$SHA`, then move the pointer:

```sh
git push origin $SHA:staging
```

A non-fast-forward refusal requires investigating the branch history. Do not
force-push or merge into a promotion pointer.

## Promote to main

After validating the staging build, advance `main` to the same tested history:

```sh
git fetch origin
SHA=$(git rev-parse origin/staging)
git merge-base --is-ancestor $SHA origin/dev
git push origin $SHA:main
```

This publishes a release candidate. Stable releases are dispatched explicitly
through `Release` on `main`. The release guard requires the commit to be on
staging, and staging commits to be on dev.

## Release notes and rollback

Roll up the changelog on a contribution branch and merge it to `dev` before
promotion:

```sh
make changelog VERSION=v0.2.0
```

Promote that commit through staging to main. Never write a release-only commit
directly onto either pointer. To roll back, publish a forward repair or select
an earlier stable release as Latest; do not rewrite branch history.

## Channel relays

Each release channel syncs through its own hosted relay Worker, deployed
automatically by `Release`'s `relay` job after `prepare` and before `publish`:

- `dev` deploys `caf-fabric-dev` (`https://caf-fabric-dev.instrument-santosh.workers.dev`).
- `staging` deploys `caf-fabric-staging` (`https://caf-fabric-staging.instrument-santosh.workers.dev`).
- `main` (`rc`) and `stable` dispatch deploy `caf-fabric` (`https://codeaf.agentfield.ai/fabric`) only when the repository variable `RELAY_DEPLOY_PRODUCTION` is `true`; while unset or anything else, the `relay` job logs that production relay deploys are off and how to enable them, contacts nothing, and lets `publish` proceed against the currently live relay.

Each deploy is gated by `scripts/relay-fingerprint.sh` (`--tag <first 16 hex>`
and `--message <commit sha>`), verified live with `internal/relay/hosted_test.go`
and `codeaf-release service --url`, and rolled back with `wrangler rollback` if
verification fails, blocking `publish`. Because a Worker code rollback cannot
undo a one-way Durable Object or storage upgrade, every relay change lands on
`dev` first and soaks on `staging` for a week before reaching `main`.
