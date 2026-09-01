---
kind: fixed
title: a context fill somebody set decides where a conversation folds; one nobody set still does not
pr: 181
surface: [chat, engine, docs]
invalidates:
  - "`--context-fill` and the `context fill` settings row governed headless leaf sizing only; the conversation's compaction trigger read the fill percentage nowhere. It reads it now — `session.compactThresholdOf` folds at `fill × trusted window` when a person set one, and at #158's derived `window − max(15%, 16k)` when nobody has."
  - "`config.contextLaw` handed `ctxbudget` the FULLY RESOLVED figure from `ContextFillAt`/`CompletionReserveAt`/`WorkingSetAt`/`ContextReuseAt`, so `ctxbudget.Limits.FillPercent` was never zero and the type's own documented law — 'zero in any field means unset' — was dead. It hands over the row as a person wrote it and zero where nobody has (`persistedCount`). The resolved answer is unchanged; the provenance now travels with it."
  - "`ctxbudget.FillPercent()` was the whole of the fill law. There is now `ctxbudget.PinnedFillPercent() (int, bool)` and `FillPercent` delegates to it; a consumer that has to tell a sixty somebody typed from the sixty aforge ships with asks that one. `session.ContextFillPinned()` is the same question at the compaction seam."
  - "`session.compactThresholdOf` WAS the arithmetic `window − max(15% of window, 16384)`. That arithmetic is `derivedThreshold` now, and `compactThresholdOf` is the picker above it that chooses between the derivation and a person's pin. Anything quoting `compactThresholdOf` as the formula is quoting the wrong function."
  - "`/status` and the phone deck named the context meter and the compaction forecast, and said nothing about the line the meter is measured against. There is a `compacts at` row directly under `context`, reading `85% of 1.3M (derived)` or `60% of 1M (pinned)`, absent when the window is unknown."
  - "The manual said the fold line follows the model's own window, full stop. It follows the window UNLESS somebody set a context fill; `compacting-over-and-over.md` carries two new sections saying so, and `docs/HEADLESS.md` says that leaving `AFORGE_CONTEXT_FILL_PCT` unset is not the same as setting it to 60."
---

Honouring the knob was never the hard part; knowing whether anybody had turned it
was. The fill percentage reached `ctxbudget` already resolved, so a sixty a person
typed and the sixty aforge ships with arrived as the same integer — and a trigger
that simply honoured it would have folded every session at sixty percent of its
window, which on the 1,310,720-token card is 786,432 rather than 1,114,112. That
is the regression #158 was written to end, which is why #158 left this out.

The settings registry had the missing fact all along, in the two ways it already
records that a person set a row: the environment variable the row names, and the
value written down in the profile. Nothing new was invented to carry it; one
resolver stopped throwing it away.

Two clamps hold a pinned line where the rest of the fold still works — the answer
room is kept above it (or the derived line, whichever is higher, so asking for
more room can never give less), and it never falls under twice the verbatim tail.
PERF.md carries both, as its own law requires.
