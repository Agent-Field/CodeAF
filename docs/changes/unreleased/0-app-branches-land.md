---
kind: added
title: branches.land merges a conversation's own task branch on a confirmed, revalidated plan
pr: 0
surface: [engine]
invalidates:
  - "No wire method could land a task branch: branches.list said a branch was unmerged and the merge was left to the person's own git. branches.land.plan {agent, branch} now binds a single-use, 120 s plan of the branch's commit, the target branch and HEAD, the resolved repository, the git configuration, every merge hook by its bytes and merge-tree's predicted tree, and branches.land {agent, plan, revision} - sent by the native host only, after its own confirmation - merges exactly that commit."
  - "A landing could be imagined to set the person's work aside the way a task's own landing does. This one never stashes, resets, aborts or retries: a dirty checkout, an untracked or ignored file where the branch adds one, a merge-tree conflict, any configured merge driver or filter, a sparse checkout, a submodule change, signing on a merge commit, merge.ff=only, branch mergeOptions and an unbounded hook are refused before anything is written, and an outcome it cannot prove is uncertain with the facts it read."
---

The branch is a key into the conversation's task records; no path, target or
ref comes from the caller. Under the repository's lock every fact is read again
and any difference is `changed`; a lock that cannot be had within 15 s is
`busy`, never an unserialized merge. The merge is `--no-autostash
--no-overwrite-ignore -s ort` on the planned commit, with a 60 s ceiling that
kills its hooks' processes with it. A write by an editor in the instant between
the last check and the merge is not serialized, and is answered `uncertain`.

Every git a land runs has automatic maintenance off, so no `gc --auto` and no
`pre-auto-gc` hook runs after the merge. A merge git refuses is `refused` only
when no hook was there to run and HEAD, the target, the index and `git status`
read back as planned; it never says nothing changed, because git has already
rewritten `ORIG_HEAD`. With a hook present it is `uncertain`.
