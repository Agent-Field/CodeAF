---
kind: added
title: A pure package that records a run's judged scores into an own sheet and outbox rows
pr: 1093
surface: [engine]
invalidates: []
---

`internal/pool/record` turns the scores `judge.Judge` answered into the two
records an install keeps of them. `Recorder.Record` observes every valid score
into the sheet it holds, under the `role_quality` metric the crew picker reads
seat quality from, and appends one row per score to its outbox when it holds
one; a nil outbox records locally only. A score whose role is not one of the
judged seats, or that is not on the 0-100 scale, is skipped and named in the
error beside the others that were recorded, and the sheet is observed whatever
the outbox does.

- `SaveSheet` and `LoadSheet` keep that sheet at `own.json` under the pool
  directory — a missing file is an empty sheet, a save is a temp-file rename
  into a 0600 file beside a 0700 directory — and `record.Cells` reads the
  sheet's plain `role_quality` cells back as the cells a prior reads, sorted
  by seat then model.
- `config.AutoOwnCells` is the start-up seam beside `config.AutoIndex`, and
  `autoPrior` folds the own cells into the index's prior seat by seat through
  `crewpick.MergePriors`, the means combined by observation count, at a floor
  of one rather than the index's min_installs: an install's own scores are its
  own evidence. A nil seam changes nothing.
- `cmd/codeaf` reads the own sheet once at start-up beside the index
  (`wirePoolIndex`); a sheet that does not parse is said under the debug
  record's switch and read as absent, not as a fault a pick stops for.
  `codeaf pool show` and `pool status` say what it holds — `own sheet: <n>
  cells, <m> observations`, or `own sheet: none` — with the same counts under
  `--json`'s `own`.
- The seat vocabulary a usage row's `seat` field carries grows a seventh
  word, `judge`, between mastermind and talk: the seat a run's judge is
  billed to, mapped from no tier and no role. No tier and no registered role
  answer it; the word reaches a row through the billing door's seat argument.
