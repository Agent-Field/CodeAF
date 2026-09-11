---
kind: changed
title: A leaf's grant is written in one place, with the measurement saying it is too small beside it
pr: 918
surface: [engine, chat, docs]
invalidates:
  - "`defaultLeafTokens` is exported as `exec.DefaultLeafTokens`, and the four Go doors that used to spell the figure themselves now read it: `aforge exec`'s `--token-budget` default, `aforge run`'s `--token-budget` default, `chatLeafTokens` in the chat surface (whose comment promised it mirrored the headless default), and `cmd/aforge`'s usage test. `internal/exec`'s `TestTheLeafGrantIsSpelledOnce` fails the build on a second spelling of the CURRENT figure anywhere under `cmd/aforge`, `internal/session`, or the non-test files of `internal/exec`. Read what that law does NOT cover before trusting it: it is blind to `internal/exec`'s own test fixtures, which choose sizes rather than restate a default; to a figure written in hex or another underscore grouping; and to a stale copy of a PAST figure, so a recalibration still owes a hand sweep for the old number."
  - "The manual is the fifth spelling and the only one a person reads: `internal/manual/chat/adaptive-runs.md` says a headless run \"stops when the run has spent 150,000 tokens\". Markdown has no integer literals for an AST law to find, so it has a prose law of its own — `TestTheManualSaysTheGrantTheLoopApplies` — which fails when that page stops carrying the current figure. A recalibration is therefore two edits, the constant and that sentence, and the build names the second."
  - "The grant's calibration — a turn costing about 11k input tokens, a well-sized leaf finishing in 8 to 16 turns — was measured against a real repository issue and does not hold: the median call carried 14.2k prompt tokens and the leaves that did the work ran 14 to 41 turns, so every one of them was landed mid-edit. The figure is still 150,000; what changed is that the constant and PERF.md now carry that reading, and the bounds that stop it being raised. Anything that treats the grant as a tuning knob is wrong — it is load-bearing, and #920 carries the work of moving it."
  - "PERF.md's \"A leaf's bounds\" opened with the ink s9 reading alone. It now opens with the 2026-09-11 measurement, the three leaves it came from, and the ceiling on the grant, so the doc a person checks their memory against says why the number is where it is."
---

Raising the grant to 250,000 was tried and turns two invariant tests red, and
that is the useful half of the reading rather than an obstacle to it. The
observation window is derived against `DefaultLeafTokens/6`, so
`TestObservationWindowIsSizedFromContextNotSpend` refuses a grant that reaches
196,608 until the window is re-derived. A 98%-cached runaway costs about 5.8×
the grant in raw tokens, so at 250,000 it reaches the 1.4M the audited
melt-downs reached and `TestACacheDiscountedRunawayLandsOnItsMoney` fails on two
separate bounds — that raw ceiling, and the cost/raw separation it closes on,
which coincides at 190,000. Two tests, three bounds, and none of them should be
moved to let a number through, so none was.
