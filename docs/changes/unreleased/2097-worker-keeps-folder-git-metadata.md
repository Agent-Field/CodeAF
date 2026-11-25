---
kind: fixed
title: a task or codeaf do worker can no longer delete the git history of the folder it was given
pr: 2097
surface: [engine, docs]
invalidates:
  - "`codeaf do --dir <repo>` and every task kept their worker inside the folder they were given, but `.git` sits inside that folder, so the folder guard read `rm -rf .git`, and direct `write` or `edit` calls aimed under `.git`, as ordinary work there and let them run. A worker that ran the delete lost the repository's history; the `git init` and `git commit` in the parent that followed were already refused, but by then nothing was left to save. `--dir` alone did not protect a repository's history. The absolute spelling `rm -rf <repo>/.git` was stopped only by the critical-command floor's `rm -rf /*` pattern, which asks about every absolute recursive delete and knows nothing about `.git`. Now any command or direct file tool that removes, moves, overwrites or rewrites the given folder's own `.git`, or removes the folder itself, is refused with the boundary's `is outside your copy` sentence, including paths reached through a symlink. Git's own commands, reading `.git`, ordinary writes in the folder, and copying or moving an ordinary file into the repository root are unchanged; a folder that is not a repository is unaffected. A headless run whose every attempted action was refused can no longer turn that into success by claiming done: it exits incomplete with the refusal in its receipt, while real recovery work after a refusal can still finish."
---

Found once on staging 17ae56d34 and not reproduced in three reruns, so this
closes the boundary rather than a path that always fires.
