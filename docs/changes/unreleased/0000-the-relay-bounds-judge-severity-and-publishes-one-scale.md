---
kind: fixed
title: The relay bounds judge severity and publishes mean and sd on one scale
pr: 0000
surface: [engine]
invalidates:
  - "`relay/src/sheet.js` fitted each judge's severity with no bound and published a cell's mean over the severity-adjusted sums while its sd was pooled from the raw ones, so a judge far enough off centre pushed a published mean past 100 — the rubric's own top — with an sd that described the unadjusted rows; the robust mean also fell back to the plain mean when the MAD was zero. Each severity is now held inside ±10 (the rubric is 100 points wide; a judge further off than a tenth of it is a different rubric, not a severity), every adjusted score is clamped back into [0, 100] before it is folded, the sd is pooled from the same adjusted triples, and the zero-MAD fallback answers the median. The priming script's all-0/100 rows under one judge id can no longer pull a published cell more than 10 points either way."
---

A severity with no bound let one judge's taste outweigh the rubric: a cell
whose raw scores all sat inside [0, 100] published 117.5 once the fitted
shift was taken out, above every score any judge had actually given, while
its sd still described the raw spread — a mean and an sd on two different
scales. The fit holds each β inside ±10 after every sweep's re-centring, and
the clamp runs at the triple level, where the store holds `{n, s, s2}` and
not the scores: the adjusted mean is `clamp((s − n·β)/n, 0, 100)` and the
adjusted sum of squares is `s2 − 2β·s + n·β²` — the exact shift, from
expanding `(x − β)²` — or `n·b²` when the mean clamps to a bound `b`. The sd
is pooled from those adjusted triples, so the published pair names one
scale. `huberMean`'s zero-MAD case answers the median: half the values sit
there, and the plain mean let the far half drag a cell below (or above) every
repeated value. Existing expectations did not move — every one of them
either fits severities inside ±10 or holds a single judge, whose β
re-centres to zero. `docs/design/model-pool/RUNBOOK.md` now states, in its
observability section, how the priming script's 0/100 rows move the fit.
