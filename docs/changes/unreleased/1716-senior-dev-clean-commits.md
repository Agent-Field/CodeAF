---
kind: changed
title: senior-dev leaves no wip commits, writes its own commit message, and gets follow-up fixes handed back
pr: 1716
surface: [chat, engine, docs]
invalidates:
  - senior-dev committed every file its model wrote or edited on the run's
    branch as it went (`wip(write): <path>`, `wip(edit): <path>`, authored
    `senior-dev <senior-dev@localhost>`), and the manual said those commits
    stay and nothing squashes them. It makes no commits of its own now; the
    run's work is the one commit codeaf makes when the run ends.
  - The `SENIOR_DEV_EAGER_COMMIT` opt-out and the `SENIOR_DEV_EXPECTED_BRANCH`
    variable codeaf passed to senior-dev existed for those commits and are
    gone.
  - codeaf's finishing commit for a program's run always took the task's title
    as its subject and the run's ending as its body. It takes the message the
    program wrote to `.senior-dev/commit-message` when there is one, and falls
    back to the title and ending otherwise.
  - The brief a program was handed said nothing about committing, so a brief
    that asked senior-dev to commit and push had it do both. codeaf's line at
    the head of the brief now tells it to leave its work uncommitted and not
    to push, switch branches or rewrite history even where the brief asks;
    this is asked, not enforced.
  - On the turn a program's run ends, the chat was told to finish a small gap
    itself on the program's branch in its own worktree. Handing the work back
    to the same program now counts as fixing it yourself and is preferred for
    anything beyond a trivial gap.
---

Prompted by #1693, whose fifty `wip` commits with senior-dev as an author would
have been written into `dev`'s history by the repository's squash merge, which
lists every commit message.
