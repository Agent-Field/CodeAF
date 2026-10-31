---
kind: fixed
title: a task or codeaf do worker can no longer delete the git history of the folder it was given
pr: 2097
surface: [engine, docs]
invalidates:
  - "`codeaf do --dir <repo>` and every task kept their worker inside the folder they were given, but `.git` sits inside that folder, so the folder guard read `rm -rf .git` as ordinary work there and let it run. A worker that ran it deleted the repository's history; the `git init` and `git commit` in the parent that followed were already refused, but by then nothing was left to save. `--dir` alone did not protect a repository's history. The absolute spelling `rm -rf <repo>/.git` was stopped only by the critical-command floor's `rm -rf /*` pattern, which asks about every absolute recursive delete and knows nothing about `.git`. Now any command that removes, moves, overwrites or rewrites the given folder's own `.git`, or removes the folder itself, is refused with the boundary's `is outside your copy` sentence, through a symlink as well. Git's own commands, reading `.git`, and every other write in the folder are unchanged, and a folder that is not a repository is unaffected."
---

Found once on staging 17ae56d34 and not reproduced in three reruns, so this
closes the boundary rather than a path that always fires.
