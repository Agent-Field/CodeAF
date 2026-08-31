# Branches

## The map

```
feature/*  ──pull request──▶  dev  ══fast-forward══▶  staging  ──tag v*──▶  release
                           (trunk)                    (soak)
                                                                main  (parked at v0.1.0)
```

`dev` is the trunk and the default branch. Every branch starts there and every
pull request goes back there. `staging` is a pointer at a commit of `dev` that
has been through the full check and is being used by people. A release is a
semver tag on a commit that is on `staging`.

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

## `main` is parked, on purpose

`main` sits at `v0.1.0` (`9716dcbf`, 17 Aug 2026) — the released v1. `dev` is
about fifteen hundred commits ahead of it.

It is *technically* fast-forwardable: `main` is a clean ancestor of `dev`, so the
promotion would work today. It is not done because it would silently hand every
v1 user a v2, and that is a product decision about a release, not a git
operation. **Until somebody decides to cut v2, `main` is not part of the
pipeline: nothing promotes to it and nothing is released from it.** `staging` is
the top of the pipeline in the meantime.

When that decision comes, it is the same fast-forward as any other, plus a tag.

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

- **No direct push to `dev`, `staging` or `main`.** Everything into `dev` arrives
  through a pull request; everything into `staging` arrives through a
  fast-forward from `dev`.
- **No force-push to any of the three.** Rolling back is re-releasing an earlier
  tag or moving `staging` forward onto a revert — never rewriting a branch other
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
