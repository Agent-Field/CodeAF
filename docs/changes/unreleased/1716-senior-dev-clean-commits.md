---
kind: changed
title: senior-dev leaves no wip commits, writes its own commit message, and gets fixes handed back
pr: 1716
surface: [chat, engine, docs]
invalidates:
  - >-
    senior-dev committed every file its model wrote or edited on the run's
    branch as it went (`wip(write): <path>`, `wip(edit): <path>`, authored
    `senior-dev <senior-dev@localhost>`), and the manual said those commits
    stay and nothing squashes them. It makes no commits of its own now; the
    run's work is the one commit codeaf makes when the run ends.
  - >-
    The `SENIOR_DEV_EAGER_COMMIT` opt-out and the `SENIOR_DEV_EXPECTED_BRANCH`
    variable codeaf passed to senior-dev existed for those commits and are
    gone.
  - >-
    codeaf's finishing commit for a program's run always took the task's title
    as its subject and the run's ending as its body. It takes the message the
    program wrote to `.senior-dev/commit-message` as it is only when the run
    passed; otherwise the run's ending follows the message before the credits.
    Missing, blank, oversized (over 8 KiB), non-UTF-8, NUL-containing,
    credit-only, non-regular or symlink messages, and unchanged copies already
    tracked at the run's start, fall back to the title and ending.
  - >-
    The brief a program in a git repository was handed said nothing about
    committing, so a brief that asked senior-dev to commit and push had it do both. codeaf's line at
    the head of the brief now tells it to leave its work uncommitted and not
    to push, switch branches, rewrite history, stash, reset, clean, or check out
    or restore files over its work even where the brief asks;
    this is asked, not enforced.
  - >-
    On the turn a program's run ends, the chat was told to finish a small gap
    itself on the program's branch in its own worktree. Handing the work back
    to the same program now counts as fixing it yourself and is preferred for
    anything beyond a trivial gap. Once retries are spent, the chat tells the
    person what still does not work, where the work is and what it would try next.
  - >-
    senior-dev's unsubmitted-run nudge and landing turn told its model to
    commit before verifying on a committed tree. They no longer do: the
    candidate is frozen from the working tree through a temporary index.
---

Prompted by #1693, whose fifty `wip` commits with senior-dev as an author would
have been written into `dev`'s history by the repository's squash merge, which
lists every commit message.
