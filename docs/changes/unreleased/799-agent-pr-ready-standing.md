---
kind: changed
title: agents are told to prove PRs with pr-ready, not full suite thrash
pr: 799
surface: [docs]
invalidates:
  - "CLAUDE.md / AGENTS.md described make pr-ready as the laptop ritual but still read like optional wording beside make check. The standing order is now explicit: prove a pull request with make pr-ready / test-touched; do not use make check, go test ./..., or a full tui3/session suite as the merge ritual on the laptop."
---

