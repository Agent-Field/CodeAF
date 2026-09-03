---
kind: fixed
title: A file counts as made only when its content changed, and a stash is said out loud
pr: 601
surface: [chat]
invalidates:
  - "A file the session had written counted as finished work whether or not its content still differed from what it was before the write. `Remains.Made` turned on the PATH being in the changed ledger, and the changed ledger records that a file WAS WRITTEN — all a revert needs to know — so a `git stash`, a revert and an edit that put a file back the way it was all left `Made` true over a tree holding none of the work. Measured on the canary of 2026-09-03 (attrs-1416, the chat cell under `--yolo`, binary b07c3dd51 which carries #534): the head edited `src/attr/_make.py` at 19:56:35, ran the suite, ran `git stash` at 19:56:56 to compare its work against the baseline, never popped it, and four seconds after the terminal reading the steward decided done — the door said `finishing here · what was asked is done` and the rig read zero changed files. NOW: the change ledger digests the file at pre-action, beside the stat it already took (`fileChange.before`, sha256, no size ceiling), and `changedInDeliverable` counts a modified path only when the content in it NOW differs from that digest. A created file counts exactly as it did; a file with no before-digest still counts, which is the conservative side."
  - "A stash was never mentioned. Nothing in the reading a goal owner decides on asked git whether the session had taken its own work out of the tree, so a run could finish over a stashed fix with a green tidy, a green reconciliation and a ledger full of paths. NOW: the terminal reading asks `git stash list` once, `Remains.Stashed` carries the count, and `Remains.unmet()` says `1 stash entry holds work that is not in the tree` (`N stash entries hold …` above one) — so a done cannot be decided over it and the carry-on brief tells the model exactly what is wrong instead of sending it to write the fix a second time. A deliverable tree that is not a git repository, and a machine with no git, are zero and say nothing. The count rides the journal's existing `checked` row as `stashed` rather than a row kind of its own."
---

Every reading this engine takes of its own work read a LIST OF PATHS. That is the
right record for a revert, which needs to know which files to put back, and it is
the wrong one for the question the terminal reading actually asks — is the work
still here. A path stays written after the tree stops holding anything it wrote.

So the one measurement that can only be taken before the write is taken there:
the ledger already stats the file at pre-action to tell a creation from an edit,
and now digests it in the same breath. And the one account of the tree that no
part of this session can take for itself is asked for out loud, once, at the
moment it can change something: git is the only party that knows a fix is sitting
in a stash, so it is asked, and what it says becomes a sentence a person and a
model can both read.
