---
kind: fixed
title: a landing that cannot save the work keeps it and asks for your look, rather than deleting it
pr: 277
surface: [engine, chat]
invalidates:
  - "a landing reported merged for work it had never committed: comeHome dropped what commitTaskWork answered, so a worktree git could not write to merged an empty branch, had its working copy force-removed and settled TaskDone with the file gone from the person's branch AND from disk. It now answers mergeAborted, merges nothing, releases nothing, keeps the branch and the worktree, and settles TaskUnverified with 'its work is in <dir> and could not be saved to its branch: <git's first line>'."
  - "a folder family that could lay only half its ledger settled done over a half-written folder: landMirror answered mergeInPlace after a failed layWork and every caller only asked whether the merge conflicted. layWork is now all-or-nothing — the whole ledger is staged beside its targets and renamed into place — landMirror answers mergeAborted, and the person's folder is left exactly as it was."
  - "stageTaskWork answered a bool and commitTaskWork a bare []string, and both were silent about failure; they now answer git's own first line, and a structural test (task_unsaved_test.go's savedWorkAnswerDropped) refuses a call site that drops the answer without a stated reason."
  - "the five landing roads each tested `merge == mergeConflicted` by hand; they ask cameHome(merge) instead, and landConflicted carries the mark it is handed rather than hardcoding mergeConflicted."
---

Issues #255 and #256, which were one defect on two roads: the failing landing and the
succeeding one answered the same outcome, so every caller downstream read "done" off a
landing that had saved nothing. What is new is not a second landing path — there is still
exactly one question a road asks of an outcome — but that a save can now say what went
wrong, in git's own words, and that a lay into somebody's own folder is atomic over the
whole ledger rather than file by file. A landing that could not put the work away leaves
it where it is and says where that is: the work on disk is then the only copy there is.
