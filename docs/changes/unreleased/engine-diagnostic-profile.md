---
kind: added
title: an opt-in, bounded CPU and heap profile of any codeaf process, for developers
pr: 0
surface: [engine, docs]
invalidates:
  - "There was no way to profile a codeaf command without editing code. CODEAF_DIAGNOSTIC_PROFILE_DIR (absolute, private) now writes a CPU profile of up to CODEAF_DIAGNOSTIC_PROFILE_SECONDS (1..60, default 30) from main, and CODEAF_DIAGNOSTIC_PROFILE_HEAP=1 adds a heap snapshot; with the directory unset nothing starts."
  - "A set-but-invalid diagnostic variable is not ignored: the process exits 2 with one fixed sentence on stderr, never on stdout."
---

See docs/DIAGNOSTIC-PROFILE.md for the guarantees and the limits.
