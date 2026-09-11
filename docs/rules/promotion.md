# Promoting, and releasing

## Choosing what to promote

**Promote the newest commit on `dev` that is at least two days old, green on the
full check, and has nothing open against it.**

Age is the criterion that matters here and it is the one people skip. The
failures this repository actually suffers are not the ones review catches — they
are the ones that surface when somebody uses the thing for an afternoon, which is
precisely what agent-written code produces and precisely what no gate can see.
A commit that nobody has run is a commit nobody has tested, however green it is.

So: people build `dev` with `make build` and use it. If a day passes and nobody
has said "something is off", that commit is a candidate.

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
gh workflow run ci-full.yml --ref $SHA

# 4. Move the pointer. Not a merge — a fast-forward.
git push origin $SHA:staging
```

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
from its `## <tag>` section in `CHANGELOG.md` and the release takes the *Latest*
badge. [changelog.md](changelog.md) says why the roll-up lands first.

To open a new minor rc line without moving `main` again, dispatch `Release` on
`main` with `channel=rc` and `component=minor`. Once an rc line is open, later rc
dispatches continue it regardless of the component choice.

`existing_version` is the repair road: dispatch on the branch required by that
channel and name an existing tag. The workflow rebuilds its commit, replaces the
assets, and reapplies the stable or prerelease marks without creating a tag.

## Rolling back

Users can pin a known tag while a repair moves forward:

```sh
curl -fsSL https://raw.githubusercontent.com/Agent-Field/aforge-v2/main/scripts/install.sh | VERSION=v0.2.0 bash
```

To make an older stable the default again without moving a branch, run `gh
release edit <tag> --latest`. For the lasting fix, revert on `dev` through a pull
request and promote the revert through `staging` and `main`. Never force-push a
pointer backwards; forward-only history is what makes a promoted commit the
same commit people already tested.
