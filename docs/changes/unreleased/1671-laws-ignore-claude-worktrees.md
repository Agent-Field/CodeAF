---
kind: fixed
title: laws ignore .claude — session worktrees no longer poison the tree-scanning laws
pr: 1671
surface: [build]
invalidates:
  - "The tree-scanning laws (taxonomy classifier, provider funnel, controller-acted list) and scripts/laws.sh's own discovery walked every directory under the repository root, so a session worktree under .claude/worktrees read as duplicated source: twelve leftover worktrees turned make test-laws red with hundreds of false hits, and any registered worktree made go test try to build a second module's packages. .claude is now skipped in every repository-rooted walker (taxonomy classifier, provider funnel, controller-acted list, exec worker and settle, config client-door, codex-client and derivation, modelsource purity, codeaf-replay, env, thread, namelaw) and in laws.sh discovery. Two macOS-only flakes are also fixed: the session landing tests resolve tempdir symlinks (/var to /private/var) and the taskcrew recovery tests skip where the platform has no /proc pressure reading."
---
---
