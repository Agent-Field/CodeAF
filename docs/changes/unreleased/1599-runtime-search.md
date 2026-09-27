---
kind: fixed
title: recursive searches skip runtime output and bound log inspection
pr: 1599
surface: [chat, engine]
invalidates:
  - "Recursive grep could read codeaf's own job output and repeat earlier searches. Both the ripgrep and walking engines now exclude known runtime logs and transcripts, even when the requested directory is inside the state home, while keeping source worktrees searchable."
  - "The walking fallback and context renderer loaded entire files. They now stream a bounded initial snapshot, skipping oversized lines; naming one log file still permits bounded inspection."
  - "Shell search guidance recommended recursive grep without runtime exclusions. Shell commands are not rewritten, so the guidance now asks for narrow source roots, explicit runtime exclusions, and byte-limited log reads."
---

The state root comes from `home.Dir()`, including custom `CODEAF_HOME` and
resolved aliases. Exclusions are applied after user globs and symlink directory
traversal is disabled. Ordinary source directories named `logs` remain visible;
only known runtime locations and legacy `.codeaf` output directories are skipped.

Recursive file search skips files over 8 MiB. Explicit single-file inspection
reads at most the first 8 MiB present at open; append activity cannot extend that
snapshot. The walking reader skips lines over 64 KiB and reports incomplete
results, and context rendering uses that same bounded reader. Requests allow at
most 1000 matches and 20 context lines per side. A ripgrep JSON response over its
1 MiB reader limit terminates and reaps the child, returning an explicit error
instead of waiting with an undrained pipe.
