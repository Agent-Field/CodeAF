# Promoting, and releasing

## Weekly staging promotion

`Promote to staging` runs every Friday after the 17:00 America/Toronto cutoff.
It chooses the newest commit on `dev`'s first-parent line whose **committer time**
is at or before that cutoff. The scheduled run may start hours late, so its
start time cannot choose the commit. `staging` moves only after a fresh reusable
`Full check` run against that exact commit succeeds. A successful push starts
`Release`; the promotion waits for its staging build and reports its tag.

The workflow sends each message to the run summary and, when configured, to
the channel attached to `SLACK_RELEASE_WEBHOOK`:

- **Moved and published:** install the named staging build with
  `curl -fsSL https://agentfield.ai/get/stageaf | bash`, or run `stageaf update`.
- **Nothing new:** staging already contains the chosen dev commit; no action.
- **Full check did not pass:** the message names the failed jobs and links the
  run. Fix `dev`, then retry from Actions.
- **Target off dev or branches diverged:** nothing moves. Inspect the branch
  pointers; never force a promotion.
- **Token missing:** the check passed, but nothing moved. Configure the token
  or use the exact by-hand push in the message.
- **Push refused:** inspect the first git error and the branch pointers before
  using the by-hand command in the message.
- **Moved but did not publish:** inspect the linked `Release` run and repair
  the release. The staging pointer has already moved.
- **Plan could not choose a commit:** inspect the linked run's fetch and plan
  step, then retry from Actions after the cause is fixed. Nothing moved.
- **Promotion step failed unexpectedly:** inspect the linked run. Its message
  says whether the push completed; if it did, inspect the staging release.

Retry with Actions → `Promote to staging`, on `dev`, leaving `target` empty for
the current dev tip; `target=cutoff` repeats the weekly cutoff choice, and a
dev commit SHA selects that commit. `dry_run=true` runs the full check and sends
the marked messages without pushing. `signal=true` also sends the production
signal. A dry run still needs the Slack webhook if the notification road is to
be exercised.

The scheduled run separately signals what the **old** staging pointer held
before this week's move. That commit has had its week on staging. The message
lists what main is behind and gives the exact fast-forward command for a person
to run; it does not move `main`. In this cadence, main runs one week behind
staging. If main already contains that staging commit, the message says so.
The date in the message is the release's `published_at` when one exists.

One-time setup: set `SLACK_RELEASE_WEBHOOK` to an incoming Slack webhook (its
configuration chooses the channel). Set `PROMOTION_TOKEN` to a fine-grained
personal access token scoped to this repository with **Contents: read and
write** and **Workflows: read and write**, owned by an account the live
`protection` branch ruleset lets bypass. List ruleset ids with `gh api
repos/Agent-Field/codeaf/rulesets`, then check from that account with `gh api
repos/Agent-Field/codeaf/rulesets/<id> --jq .current_user_can_bypass`; it must
answer `always`. A GitHub App would need a token-minting step because its
installation tokens expire after one hour; this workflow has no such step.
`GITHUB_TOKEN` cannot start the downstream release workflow on push and cannot
push commits that change workflow files.

## Choosing what to promote

The weekly promotion chooses the Friday cutoff commit after a fresh full check.
It does not inspect a commit's age or open issues. The older two-day soak rule
guides the by-hand fallback: choose a dev commit people have used for at least
two days and against which nothing is open. A dispatch can name that SHA while
it is still ahead of staging; a forward-only pointer cannot
move back to an older commit after a promotion.

People should build `dev` with `make build` and use it during the week. The
failures that surface after an afternoon of use are not all caught by a gate.
That human use informs whether to repair dev before Friday or select a specific
dev commit by hand. The scheduled job still follows its cutoff and Full check
rule, so it never silently substitutes a human judgment for either one.

## dev → staging

```sh
git fetch origin

# 1. Pick the commit, and look at what it is.
SHA=$(git rev-parse origin/dev)                 # or an older, better-soaked one
git log --oneline origin/staging..$SHA          # what this promotion contains

# 2. It must be on dev. This is the whole safety property; check it, do not
#    assume it.
git merge-base --is-ancestor $SHA origin/dev && echo "on dev"

# 3. Run the full check against it first, so a red staging is never how you
#    find out. This is the same workflow CI runs.
gh workflow run ci-full.yml --ref dev -f ref=$SHA

# 4. Move the pointer. Not a merge — a fast-forward.
git push origin $SHA:staging
```

Wait for the dispatched Full check to conclude `success` on `$SHA` before
running step 4. A queued dispatch is not a passed check.

If step 4 is rejected as a non-fast-forward, **do not force it.** It means
`staging` is somewhere `dev` has not been, which should be impossible and is
worth understanding before anything else happens.

## staging → main

`main` moves by the same deliberate fast-forward, after the chosen commit has
soaked on `staging`:

```sh
git fetch origin
SHA=$(git rev-parse origin/staging)
git merge-base --is-ancestor $SHA origin/dev && echo "on dev"
git push origin $SHA:main
```

That push publishes the next rc automatically. If it is rejected as a
non-fast-forward, do not force it; understand why `main` contains history that
is not behind `staging`.

## Releasing

**Roll the changelog up first, on `dev`, through a pull request.**

```sh
make changelog VERSION=v0.2.0        # writes CHANGELOG.md, eats the loose entries
```

That has to land on `dev` and be promoted like anything else — never committed
onto a pointer. The order is: roll up on `dev`, promote that commit to `staging`,
let it soak, then fast-forward `main`. The push to `main` automatically publishes
the next `vX.Y.Z-rc.N` prerelease. The workflow refuses it unless the commit is
already on `staging`.

Cut stable by opening Actions → `Release` on `main`, choosing `stable`, and
dispatching it. `component=patch` closes the highest open rc line; `minor` or
`major` starts that new stable line from the latest stable tag. Stable notes come
from its `## <tag>` section in `CHANGELOG.md` — shortened to the headline lines
when the section is bigger than a release body may be — and the release takes
the *Latest* badge. [changelog.md](changelog.md) says why the roll-up lands first.

To open a new minor rc line without moving `main` again, dispatch `Release` on
`main` with `channel=rc` and `component=minor`. Once an rc line is open, later rc
dispatches continue it regardless of the component choice.

`existing_version` is the repair road: dispatch on the branch required by that
channel and name an existing tag. The workflow rebuilds its commit, replaces the
assets, and reapplies the stable or prerelease marks without creating a tag.

Every release carries `THIRD-PARTY-NOTICES.md` beside its binaries, inside the
same `checksums.txt`; `go run ./cmd/codeaf-notices generate` refreshes it when
the dependencies move.

## Rolling back

Users can pin a known tag while a repair moves forward:

```sh
curl -fsSL https://raw.githubusercontent.com/Agent-Field/codeaf/main/scripts/install.sh | VERSION=v0.2.0 bash
```

To make an older stable the default again without moving a branch, run `gh
release edit <tag> --latest`. For the lasting fix, revert on `dev` through a pull
request and promote the revert through `staging` and `main`. Never force-push a
pointer backwards; forward-only history is what makes a promoted commit the
same commit people already tested.
