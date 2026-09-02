---
kind: fixed
title: an empty profile directory is the ordinary launch, so the crew draws and the notices remember
pr: 320
surface: [chat]
invalidates:
  - "The status line showed the crew on an ordinary launch — it did not, since crewSegment guarded on an empty profileDir, and AFORGE_PROFILE_DIR is unset on very nearly every launch. It guards on a hosted window now, so `crew balanced` is on the row for everybody."
  - "An empty profileDir meant a surface with no profile. It never has: internal/config resolves \"\" to this process's own profile in the state root, and the only window with no profile of its own is a hosted (`--host`) one."
  - "The notices' ledger was written per profile. It was written for nobody: noticeLedgerPath answered \"\" on an ordinary launch and the board kept everything in RAM, so a hint retired by its own gesture came back at the next launch, a hint ignored was never counted past session one of three, and the news channel never had an older build to compare against. All three now work, which is a visible change: a hint you already acted on will stop coming back."
  - "config.BudgetConfigPath was the place that knew what an empty profile directory means. config.ProfilePath is, and BudgetConfigPath, the notice ledger and cmd/aforge's chatLogPath all resolve through it."
  - "The manual said the crew segment is absent on a session with no profile. It is absent only on a remote (`--host`) session, whose crew is the other machine's."
  - "internal/tui3's tests ran against the machine's own state root. They now run under a TestMain that points AFORGE_HOME at a temporary directory, because a bare test app names no profile and would otherwise read the developer's crew and retire their hints."
---

Three sites read an empty profile directory as "there is no profile" and went
quiet on exactly the launch they were written for. A fourth found by the sweep —
the greeting's crew clause — did the same. The three left standing are named in
`internal/tui3/emptyprofile_test.go` with the reason each was left, and one of
them is worth its own issue: `openSetup` returns before opening the first-run and
provider-connection screens on any launch that has not exported
`AFORGE_PROFILE_DIR`, which is almost all of them.
