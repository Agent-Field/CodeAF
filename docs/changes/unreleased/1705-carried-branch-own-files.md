---
kind: fixed
title: a carried-on run counts only its own files, and a published branch is pushed, not merged
pr: 1705
surface: [engine, chat, docs]
invalidates:
  - "A senior-dev run that carried on an earlier run's branch counted its files from where the line's first run began, so a branch rebased onto newer history between runs (for its pull request) had a run that changed 13 files end saying 117. It now counts from where the branch stood when the run began (`N files past <commit>, where the last run left it`), and one that adds nothing says `it added nothing to the branch <branch> in <folder>, which still holds the earlier runs' work as the last run left it`. The branch is still kept and still lands as the line's."
  - "That ending told the person to `git stash` and `git merge` the branch into their own checkout even when it tracked a remote branch with an open pull request. A branch with an upstream now says it tracks `origin/<branch>` and that `git -C '<folder>' push origin <branch>` sends this work there, with no stash and no merge; and for a carried branch with no upstream, the stash advice appears while the branch still begins with the person's uncommitted changes, rebased since or not, and goes once it does not."
---
