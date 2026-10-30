---
kind: added
title: do --no-resident refuses the live-resident hand-off a script cannot measure
pr: 1959
surface: [docs, engine]
invalidates:
  - "A `codeaf do` run on a machine with a live resident always handed its brief over to that resident; with --no-resident (CODEAF_NO_RESIDENT=1) it now refuses with exit 1, names the holder, and writes nothing to the journal."
---

The resident hand-off compiles the brief conversationally in the resident's own
head, which is the right thing for a person and a different run than a script
asked for. `--no-resident` is the opt-out: probe the lock, refuse while a live
non-stuck holder serves the store, and leave no command behind for it to apply.
Running truly headless beside the resident (isolated store) stays out of scope.