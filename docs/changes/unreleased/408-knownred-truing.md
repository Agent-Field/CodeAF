---
kind: internal
title: five green tests leave the known-red ledger
pr: 408
surface: [build]
invalidates:
  - "`cmd/aforge TestHarnessEntriesFromStore` was listed in `.github/known-red.txt` and CLAUDE.md as failing on a clean tree. It is deterministic and green; CI runs it again."
  - "`internal/tui TestSettingsNavigatesAndEditsEveryKindAndPersists` and `TestSettingsRefusesToFightTheEnvironment` were listed as failing on a clean tree. Both have been green since ab6a978d (2026-08-27); CI runs them again."
  - "`internal/remote TestTheEnginesStandingStoreAnswersOverTheWire` was listed as red on every platform and owed a real fix. It has been green since 151b3403 (2026-09-01); CI runs it again."
  - "`internal/resident TestATransientClaimFailureDoesNotEndDispatchForever` was listed as environment-shaped red on Linux. The test was rewritten in 05b99537 (2026-09-01) and is green; CI runs it again."
---

The gate skips every name on the ledger, so a fixed test stays skipped until
somebody notices. These five were fixed days ago and nothing re-ran them.
