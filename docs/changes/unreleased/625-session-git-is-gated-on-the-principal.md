---
kind: fixed
title: A run left on its own answers to the same git list a task does
pr: 625
surface: [engine, chat]
invalidates:
  - "A session's own git was never refused. The list of verbs a task may not run — `stash`, `merge`, `rebase`, `cherry-pick`, `revert`, `checkout`, `switch`, `am`, `apply`, `worktree`, `update-ref`, `symbolic-ref`, `pull`, `fetch`, `clone`, `remote`, `submodule`, `push`, `reset --hard|--merge|--keep`, `restore --source` — existed and was right, and nothing consulted it outside a task: `taskGitGuard.PreAction` returned early whenever `Config.InTask` was false, which is every conversation, an unattended `--yolo` run included. NOW the gate is the principal and not the flag: a session whose principal is a `Steward` (unattended, with a ceiling) answers to that same one list, refused on the same pre-action seam, before the shell runs."
  - "The refusal was written in one register, the task's, and it said `only what you write here comes home` and `this task's work comes home through its landing`. Both are false about a session that has no landing and whose work is judged where it stands, so there are two registers over ONE list of verbs and one scanner — `refusedTaskGit` keeps exactly one non-test call site and a structural law counts it with `go/ast`, so the two postures cannot drift into two lists. A run left on its own reads `git stash is not yours to run here: it takes your working copy away, and what is in it is the work this session will be judged on. Leave the change in the tree, or commit it`, which is the sentence the manual quotes."
  - "`how-tasks-run.md` said `None of this applies to you` about every conversation. It applies to a session you left running with a budget; it does not apply to a session you are sitting in front of, or to an unattended run that named no ceiling — both of those keep a `Person` and run whatever git you ask for, unchanged."
  - "#601 said the stash out loud AFTER the fact — the terminal reading counts the entries a run put there itself and refuses to call the work done over them. That reading stays exactly as it is, and there is now usually nothing for it to say, because the verb no longer runs. `gh pr create` and a hand-rolled request to the forge are still refused for a task alone (`refusedTaskReach` remains gated on `InTask`); only the git list moved."
---

The canary that wrote this: the head edited the fix into `src/attr/_make.py`, ran
`git stash` twenty-one seconds later to compare its work against the baseline,
never popped it, and four seconds after the terminal reading the door said
`finishing here · what was asked is done` over a clean tree. Nothing it did was
against a rule — the rule was written for a task, and this was a session doing a
worker's job on its own word.
