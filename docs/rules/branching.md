# Branches

## The map

```
feature/*  ──pull request──▶  dev  ══fast-forward══▶  staging  ══fast-forward══▶  main
                           (trunk)                    (soak)                       (release)
                              │                          │                            │
                           dev build                staging build                 rc / stable
```

`dev` is the trunk and the default branch. Every branch starts there and every
pull request goes back there. `staging` is a pointer at a commit of `dev` that
has been through the full check and is being used by people. `main` is the
release pointer at a commit already on `staging`. Pushes to the three pointers
publish dev, staging and rc builds respectively; a stable release is dispatched
on `main` by a person.

## Why promotion is a fast-forward and not a merge

**`staging` and `main` are never merged into. They are moved.**

```sh
git push origin <a-sha-already-on-dev>:staging
```

Three things follow from that, and all three are the reason:

- **There is no divergence to reason about, ever.** `main` is an ancestor of
  `staging` is an ancestor of `dev`. Nobody has to ask which branch is the truth,
  nobody cherry-picks a fix "back", and there is no back-merge to forget.
- **A promotion is a decision about one commit, not about a diff.** You use build
  `abc123` for two days and then promote `abc123` — the exact bytes that were
  tested. Merge-based promotion creates a merge commit that nobody has ever run.
- **The server can enforce it.** A fast-forward-only rule
  (`.github/rulesets/promotion-pointers.json`) rejects any push that is not a
  descendant. Unreviewed code cannot reach `staging` by accident, only by
  somebody switching the rule off in front of witnesses.

The cost is that promoting is a deliberate act rather than a merge button, which
for a soak branch is the point. [promotion.md](promotion.md) is the runbook.

## `main` is the release branch

Since 2026-09-10, `main` is the last fast-forward pointer in the pipeline. Moving
it publishes a release candidate from exactly the commit that soaked on
`staging`; dispatching the `Release` workflow there cuts the stable version.

The old reason for parking it was sound: moving `main` would hand v2 to every v1
user. That is now an explicit release decision rather than an indefinite hold.
The stable dispatch makes that decision on purpose, while an rc on every `main`
push gives the same bytes a public trial first.

## Naming, and how long a branch lives

Short-lived, off `dev`, deleted when merged (GitHub does the deleting):

- `feat/<thing>` — new capability
- `fix/<thing>` — a repair
- `docs/<thing>` — prose only
- `chore/<thing>` — build, CI, tooling

The wave lanes this repository builds in — `git worktree add ~/af-<name> -b
<branch>` — follow the same rule: they are short, they merge, they go. **A branch
that has been open long enough to need rebasing twice is a branch that should
have been split.**

## What is forbidden

- **No work is pushed directly to `dev`, `staging` or `main`.** Everything into
  `dev` arrives through a pull request; the only direct updates to `staging` and
  `main` are the deliberate fast-forwards in the promotion runbook.
- **No force-push to any of the three.** Rolling back is marking an earlier
  stable Latest or promoting a revert forward — never rewriting a branch other
  people have pulled.
- **No long-lived integration branch besides `dev`.** Two trunks is two truths.
- **No `git add -A` and no `git add .`** — several sessions work this tree at
  once and a wildcard sweeps another lane's in-flight files into your commit.
  Stage your own explicit paths. This one predates the branch rules and is in
  `CLAUDE.md` for the same reason.

## Merges are squashes

Squash is the only merge method the repository allows. A pull request becomes one
commit on `dev`, which is what makes a promotion SHA a thing a person can reason
about and a revert a single `git revert`.
