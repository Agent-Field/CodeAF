---
kind: fixed
title: A kept branch is delivered only when the check could find the files it compared
pr: 682
surface: [chat, engine]
invalidates:
  - "The retained-delivery reading treated a clean `git diff --quiet` over a task's changed paths as proof the work had reached the workspace. Exit 0 also means the pathspecs matched NOTHING, so a finished task whose changed list named paths that exist neither on its kept branch nor in the workspace was reported delivered and an unattended conversation ended on `finishing here · what was asked is done` over work still sitting unmerged. A clean diff now releases the branch only when at least one listed path is really on the branch or really tracked in the workspace; where none is, the work stays retained and the ending still says `… is on retained branch <branch>; its changes have not reached the requested workspace`."
  - "A conversation standing in a SUBDIRECTORY of a project was the ordinary way into that: its task worktree is cut at the repository root, so every path the node recorded was spelled against a root the delivery reading does not stand in and none of them could be compared. That shape now reads as undelivered rather than as done."
---

The changed list a node leaves is a claim and not a receipt — `TaskNode.finish`
writes it with no reconciliation and `taskTree.comeHome` throws away the paths
`commitTaskWork` actually saved — so the guard sits at the decision rather than
at the ledger: after the diff comes back clean, the two sides it compared are
read with the same pathspecs (`git ls-tree` for the kept branch, `git ls-files`
for the workspace) and a listing counts only when it names a path that was asked
about. A list holding one real difference still exits 1 and stays retained; a
workspace that genuinely holds the branch's content still reads as delivered.
Reconciling the ledger itself is left alone.
