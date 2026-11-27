---
kind: fixed
title: In the desktop app, agents find gh and Homebrew's tools without any setup
pr: 3001
surface: [app, engine, docs]
invalidates:
  - "The engine kept the minimal PATH the desktop app starts it with, and the agent's login shell never reads `~/.zprofile` where Homebrew's `shellenv` lives, so `gh`, `brew` and other Homebrew tools read as command not found inside agent shells, while the engine's own `gh` lookups found them through a fixed fallback list. Now `codeaf app` asks `$SHELL -l -c` for its PATH once at start (three seconds at most) and appends the folders its own PATH lacks, then the usual Homebrew and `~/.local/bin` folders that exist; it never reorders or drops an entry, and a failed probe adds only the usual folders. Windows is unchanged."
---

Only the desktop engine does this. A terminal run already has the PATH of the
terminal, and the `gh` lookup keeps its fallback for every other start.
