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

## Releasing

A release is a semver tag on a commit that is on `staging`, cut by a person.

```sh
git fetch origin
git tag -a v0.2.0 origin/staging -m "AForge v0.2.0"
git push origin v0.2.0
```

That, and only that, publishes. There is no branch trigger anywhere in
`.github/workflows/` — a merge landing on a branch never becomes a download
somebody else installs. `release-binaries.yml` also **refuses to publish a tag
whose commit is not on `origin/staging`**, because tagging is the one step of the
promotion that no branch rule can police.

`v0.2.0` takes the *Latest* badge. Anything else — `v0.2.0-rc.1` — publishes as a
prerelease and leaves *Latest* pointing at the last stable one.

`workflow_dispatch` on the same workflow builds the six binaries and uploads them
as workflow artifacts without publishing. Use it to look at a build.

## Rolling back

**In order of preference, and the first two are almost always the answer:**

1. **Re-release the previous tag.** The product ships as tagged binaries, so
   "roll back production" means pointing people at `v0.1.0` again. No branch
   moves; nothing anybody has pulled is rewritten.
2. **Revert on `dev`, then promote forward.** `git revert` the squashed commit,
   let it go through the normal pull request, then fast-forward `staging` onto
   the result. `staging` only ever moves forward, which is what keeps it
   trustworthy.
3. **Never `push --force` a branch to an earlier commit.** It rewrites history
   other people and other machines have already pulled, and it is the one action
   the fast-forward rule exists to prevent.

## main

`main` is parked at `v0.1.0` and is not part of this. Nothing promotes to it,
nothing releases from it. [branching.md](branching.md#main-is-parked-on-purpose)
says why and what changes when v2 is cut.
