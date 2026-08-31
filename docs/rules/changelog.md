# The changelog

## What it is for, which is not what a changelog is usually for

**Every pull request into `dev` carries one file describing what it changed.**

The reader this is written for is a session opening this repository with a
fortnight-old memory of it — a memory formed from `CLAUDE.md`, a design doc, a
manual page, and whatever it saw the last time it was here. Most of that memory
is still right. The part that is not is expensive out of all proportion: it does
not read as a gap, it reads as knowledge, so it is acted on with confidence and
nobody notices until the push fails or the wrong page gets edited.

That has already happened here more than once. `CLAUDE.md` ordered finished work
pushed to `origin chat-v3-task` for days after that branch stopped existing.
`generate_image` was documented as impossible right up until the wave that
shipped it. In both cases the code was correct, the tests were green, and the
thing that was wrong was what somebody remembered.

**So the changelog's job is to carry the invalidations.** Not what shipped —
`git log` says that and says it better, and an entry that only restates the
commit subject is an entry that cost somebody a minute and earns nobody anything.
What shipped is the headline. What is no longer true is the payload.

## The shape

```markdown
---
kind: changed          # added changed renamed fixed removed internal
title: dev is the trunk, staging is a pointer, and nothing publishes by itself
pr: 82
surface: [build, docs] # optional: chat engine resident remote build docs
invalidates:           # optional, and the reason the file exists
  - "The trunk was `chat-v3-task`. It no longer exists on origin; branch off `dev`."
---

Optional paragraph on why, if the title does not already carry it.
```

Five fields, usually three. **A form long enough to resent is a form that gets a
placeholder typed into it**, and a changelog of placeholders is worse than none,
because it reads as though somebody checked.

`renamed` is its own kind rather than a flavour of `changed` because a name that
moved — a branch, a tool, a command, a flag, a file — is the single most common
way a stale memory turns into a wasted hour.

## Writing `invalidates`

This is the only field with a quality gate, and the gate is that **each line has
to be a claim somebody can check their own memory against.** Two sentences, or
one with a semicolon: what it was, and what it is now.

| No | Yes |
| --- | --- |
| `branch names` | `The trunk was `chat-v3-task`. It no longer exists on origin; work goes to `dev`.` |
| `changed the step default` | ``propose_task``'s step default was 40 in the schema and 200 in the executor. Both are 200.` |
| `image generation` | `The manual said aforge cannot generate images. It can: `generate_image` is on the belt.` |

The tool refuses a line that does not end in a full stop or is under 25
characters, which catches labels. It cannot catch a full sentence that says
nothing, and neither can review — that one is on you.

**Leave the list empty when nothing anybody believed changed.** Most fixes are
like that, and an empty `invalidates` is an honest answer, not a skipped field.
Inventing one to look thorough is the failure mode that kills the field, because
a reader who opens two folds of noise stops opening them.

## When you owe one, and when you do not

Every pull request into `dev`, enforced by the `check` job. The escape hatches,
in order of preference:

1. **`kind: internal` with just a title.** Ten seconds, and it keeps the log
   complete — a model *does* want to know that the task executor was refactored
   under it, even with nothing to invalidate.
2. **The `no-changelog` label** on the pull request. For a typo in a comment.
   Reach for it rarely; a repository where half the changes are unlogged has an
   unreliable log, which is worse than an absent one.

There is no exemption for "it is only docs". A docs change is frequently the
*most* invalidating thing in a wave, because it moves what everybody reads.

## Why it is not generated

Three ways this could have been automated, and why none of them survives:

- **From `git log`.** It would produce `git log`. The one field worth having is
  the one a diff cannot contain.
- **From the pull-request body at merge time, by a bot.** It would need to push
  to `dev`, which `branching.md` forbids, and it would still be guessing at the
  invalidations.
- **By a model reading the diff after the fact.** This is the tempting one, and
  it is wrong in a specific way: the model reading the diff can see what changed
  but not what anybody *previously believed*, which is the whole content. The
  author knows they are contradicting a page, because they had to go and edit it.

What is automated is everything that does not need judgment: validation,
grouping, the two-layer rendering, the roll-up, and the release notes.

## One file per pull request, not one appended file

Several sessions work this tree at once. A shared `CHANGELOG.md` that every
branch appends to conflicts on every merge, and a step that reliably produces a
conflict is a step people reliably route around. Loose files never collide — the
name carries the pull-request number — and they are easier for a model to filter
than a parsed section of one long document.

## The commands

```sh
make changelog-new PR=82 KIND=changed SLUG=branch-rules   # scaffold
make changelog-check                                      # validate; CI runs this
make changelog-preview VERSION=v0.2.0                     # see the rendered section
make changelog VERSION=v0.2.0                             # roll up, at release time
```

`make changelog` writes the section into `CHANGELOG.md` and deletes the loose
entries it consumed. **It is run on a branch and lands through a pull request
into `dev` before the promotion** — never as a commit on `staging`, which would
break the fast-forward that `branching.md` depends on.
[`promotion.md`](promotion.md) has it in order.

The release workflow reads the section back out of `CHANGELOG.md` for the release
notes, so the roll-up is not paperwork after the fact: it is the release notes.
