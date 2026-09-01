---
kind: changed
title: a parent that divides freezes its world once, and every part starts from that freeze
pr: 246
surface: [engine]
invalidates:
  - "Each part of a division sealed the parent's tree independently when its worktree was cut. A parent that kept writing between those cuts handed its siblings different worlds. `divideOnce` now seals once (`the world this division starts from: <title>`) and `snapshotRung` cuts from that SHA instead of sealing again."
  - "A part's `taskStand` carried a directory and a mode. It now also carries the parent's divide-time freeze, so `prepareTaskTreeOn` does not have to guess when the family's world stopped moving."
---

The sealer is still `sealGroundWork`. What changed is when it runs (once, at
the split) and that later cuts honour that commit even if the parent kept
writing. A folder that is not a repository is left alone — no `git init` in
the person's directory. #230 is what turns a folder-ground mirror into a repo.
