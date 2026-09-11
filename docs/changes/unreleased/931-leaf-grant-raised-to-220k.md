---
kind: changed
title: A leaf's grant is 220,000, measured, with the band it sits in written down
pr: 931
surface: [engine, chat, docs]
invalidates:
  - "`exec.DefaultLeafTokens` is **220,000**, not 150,000. Every door reads it, so `aforge exec --token-budget`, `aforge run --token-budget` and the chat surface all changed with it, and a run that used to be landed mid-edit at 150,000 now finishes. The old figure was calibrated from a turn costing about 11k input tokens and a leaf finishing in 8 to 16 turns; measured against issue #898 run headless on `z-ai/glm-5.3` (run `04c2404b26072e41`), the median call carried 14.2k prompt tokens and the leaves ran 14 to 41 turns, and all three were landed mid-edit. The cost of that was not the stop — it was five planning rounds and seven nodes for one issue, 43 minutes and $2.11, with the work correct and committed after the first two leaves."
  - "The grant's band is 200,000 to about 241,000 and it is now written down in three places that must move together. The floor is `TestACacheDiscountedRunawayLandsOnItsMoney`'s closing comparison, which stops separating at 190,000 where the simulated cost bound and the real run both land at 85 turns. The ceiling is the same test's raw bound: at 98% caching a runaway costs about 5.8× the grant in raw tokens, so 242,000 reaches the 1.4M the audited melt-downs reached. **No assertion was weakened to make room.** Anything that remembers the grant as untouchable, or as free to raise, is wrong in both directions — it moves inside a measured band, and moving it means re-running #920's sweep."
  - "`TestObservationWindowIsSizedFromContextNotSpend` no longer appears to bound the grant under 196,608. It compared the context-derived window against `DefaultLeafTokens/6`, but the window has not read the grant since `observationWindow` became `ctxbudget.ObservationBytes` of the model's own context — the division was the test recomputing a HISTORICAL figure, what the spend ceiling used to buy, from a number that had since moved, so it failed whenever the grant ROSE. That constant is frozen at the 25,000 it actually was and the assertion is unchanged. If you read #918 or #920 and remember three invariants pinning the grant, there are two."
  - "`docs/HEADLESS.md`'s `--token-budget` row said `150000` and was the seventh place this figure lived — the copy that survived #918, found by hand. `internal/exec`'s new `TestTheHeadlessReferenceStatesTheGrantTheDoorsApply` reads that row and fails the build when it disagrees with the constant, so the reference a person reads instead of `--help` is now on a gate. `internal/manual/chat/adaptive-runs.md` already had one through `internal/manual`'s truth table."
---

The band is 41,000 wide and 220,000 sits inside it with room on both sides,
which is the whole reason to write the band down rather than only the number:
the next person to reach for this has a floor, a ceiling, and a replication that
produces both. `PERF.md`'s "A leaf's bounds" carries the same reading, as the
rule that a changed cap changes the doc in the same commit demands.
