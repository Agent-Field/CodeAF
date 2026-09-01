---
kind: fixed
title: resource policy is derived from what the process measured, not from constants
pr: 158
surface: [chat, engine]
invalidates:
  - "The compaction machinery clamped every claimed context window to 256,000 tokens (`maxTrustedWindow`, twice the default), so a model advertising 1.3M folded at 217,600 and the manual said so in as many words. That ceiling is gone: the threshold follows the model card's own window, and the only thing that lowers it is an endpoint REFUSING a prompt for being too long — recorded per model in `model-quirks.json` (`provider.NoteServedWindow` / `ServedWindow`) and applied by `session.TrustedWindowFor`."
  - "A child agent — a task node's worker, an adaptive run's worker, a forked hand, an auditor — running on a model other than the conversation's was handed `ContextWindow: 0` outright, and so folded against the 128,000-token default however much room its own model had. It is handed that model's card figure now (`Agent.childWindow`), and `Config.ContextWindowFor` is inherited by every child so a worker that switches its own model can still learn that model's window."
  - "`session.CompactThreshold(window)` was the whole law and `internal/tui3` called it with the window alone. There is now `CompactThresholdFor(model, window)` and `TrustedWindowFor(model, window)`; the surface's meter calls the model-aware door so it cannot draw a different figure from the one the trigger uses."
  - "`streamWallFloor`, five minutes, was the floor under EVERY stream wall, so a lane whose longest finished reply was twenty-four seconds still waited out five whole minutes and the derivation could never make anything shorter than a stranger got. It is now the outer bound for a lane with NO history only; a measured lane is floored at `streamWallMeasuredFloor` (= `bufferedQuietBound`, 2m30s)."
  - "The mid-stream silence bound was a flat 45 seconds for every endpoint. It is 45 seconds only for a lane whose rate has not been measured; a measured lane is cut at `gapFor(rate)` — the time IT takes to write `streamGapLumpTokens` (3,500) — clamped to `LagGap`…`midStreamGapBound`, so 250 tok/s waits 15s. The keepalive extension window stays the flat bound at every rate."
  - "`lane.Ledger.NoteOutcome` and the whole quality axis behind it — `Belief.Quality`, `Beta.Observe`, `Beta.Toward`, `QualityHalfLife`, the frontier's `QualityNeed` gate — had NO production caller: the only non-test call in the tree was `bench/lanelab`'s simulator, so a lane's quality belief never left its prior. Every guard cut (silence, stall, overrun, soup, unparsed tool grammar) and every answer with nothing in it is now reported as an outcome that was not accepted, and every answer that survives every guard as one that was."
  - "A lane row in the model picker's fold could say `no tools`, `out ≤ 65k`, `fp4` or `tail 12s`. It can now say `bad replies`, and that note outranks all four."
  - "`internal/lane` exports `QualityPrior`, so a surface ages a quality belief with the same prior the chooser decays toward."
---

Three symptoms of one defect, all measured in the 2026-08-31 self-build dogfood run
(#141): the conversation compacted nineteen times at ~80–100k tokens on a model
advertising 1.3M, two streams hung for the full five minutes on endpoints sustaining
83–270 tok/s, and an endpoint that served a reply which had stopped being language was
retried on equal footing straight afterwards.

In every case the measurement already existed and a constant outranked it. The window was
clamped before it was read; the wall's floor was larger than anything the wall could
derive; and the belief that ranks endpoints had a quality axis with nothing wired to it.
