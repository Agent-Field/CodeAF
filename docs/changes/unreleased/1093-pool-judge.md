---
kind: added
title: A pure package that scores a finished run seat by seat, by a judge outside the crew
pr: 1093
surface: [engine]
invalidates: []
---

Standard library and `internal/catalog` only, no disk, no network, and nothing
on a surface wired to it yet.

- `internal/pool/judge` reads a run's record — its brief and deliverable, its
  report, claim and ending, the files it wrote and the checks it ran, and which
  model held each of the worker, high and mastermind seats — and asks one
  question per seat, in role order, through a caller-supplied `Ask`. Each answer
  is read as one JSON object carrying a 0-100 score and a one-sentence reason,
  on the same scale crewpick reads seat quality on and a pool records its
  `role_quality` metric in. A seat whose model is the judge's own is skipped; a
  seat whose answer cannot be read fails alone, and the scores obtained come
  back beside an error naming it. The caller owns the model, the transport and
  the bill, and bills the judge's calls to its own seat.
- `judge.Pick` chooses that judge from a catalog: the cheapest row whose
  published coding index reaches `judge.DefaultFloor`, that carries tools, that
  no crew seat holds and whose vendor is not the worker's, ties broken by id.
