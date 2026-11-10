---
kind: fixed
title: exec no longer leaves its .codeaf scratch inside the person's repository
pr: 2571
surface: [build, docs]
invalidates:
  - HEADLESS.md documented that exec roots its scratch (.codeaf/trace, .codeaf/jobs, .obs) inside the --dir folder, and a plain exec left `?? .codeaf/` in `git status --porcelain`; the scratch now lands outside the repository under the codeaf state root, and a plain exec leaves `git status --porcelain` empty.
---

The standalone `exec` door never moved its scratch out of the workspace the way
the `do` door does, so the turn trace, job logs and .obs spills appended to
untracked files in the person's repository — a `git add -A` would publish them.
Repos already dirtied by pre-fix runs keep their old untracked `.codeaf/` until
the person removes it; this fix stops new writes into the workspace.
