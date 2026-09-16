---
kind: removed
title: the internal experiment records leave the tree, and the design docs stop naming the bench machine
pr: 1068
surface: [docs]
invalidates:
  - "`docs/design/pin-doe/`, `docs/design/workspace-foundation/` and the day-by-day wave records under `docs/design/conversation-runtime/` (status, plans, audits, local change notes, the prototype) were in the tree. They are gone; `docs/design/conversation-runtime/PARETO.md` stays because `bench/conversation/README.md` cites it as the measurement contract. `docs/design/plan-gate-doe/REPORT.md` and `docs/design/turn-wall-share-doe/` stay too: live comments in `cmd/codeaf`, `internal/config`, `internal/session` and `internal/splitgate` point at them as the measured evidence behind a default."
  - "Bench READMEs and the design docs under `docs/design/` spelled the bench machine by name (`ssh spark`, `~/.config/fleet/secrets.env`, `/private/tmp/af-conversation-ops/…`). Those pages now say `ssh benchhost`, a sourced `secrets.env` and a scratch bench-ops directory. `CLAUDE.md`, the `Makefile`, `docs/rules/ci.md`, `BENCHMARKS.md` and code comments were not part of this pass and still say Spark."
  - "`test/ux/` was removed by #991 because it drove the v1 and v2 surfaces. A revival of it was proposed here and withdrawn: nothing ran it, it had never been run, and when it was run its first journey failed on its first screen check (#1072). `internal/e2e` remains the one suite that drives the binary, as `docs/JOURNEY.md` says."
---
