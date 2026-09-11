---
kind: changed
title: A leaf's grant is written in one place, with the measurement saying it is too small beside it
pr: 918
surface: [engine, chat, docs]
invalidates:
  - "`defaultLeafTokens` is exported as `exec.DefaultLeafTokens`. The figure used to be spelled again at three doors — `aforge exec`'s `--token-budget` default, `aforge run`'s `--token-budget` default, and `chatLeafTokens` in the chat surface, whose comment promised it mirrored the headless default — and a fourth time inside `cmd/aforge`'s usage test. All of them read the constant now, and `internal/exec`'s `TestTheLeafGrantIsSpelledOnce` fails the build on a second spelling anywhere under cmd/aforge, internal/exec or internal/session. A lane that recalibrates the grant changes one number, not five."
  - "The grant's calibration — a turn costing about 11k input tokens, a well-sized leaf finishing in 8 to 16 turns — was measured against a real repository issue and does not hold: the median call carried 14.2k prompt tokens and the leaves that did the work ran 14 to 41 turns, so every one of them was landed mid-edit. The figure is still 150,000; what changed is that the constant and PERF.md now carry that reading, and the three invariants that stop it being raised. Anything that treats the grant as a tuning knob is wrong — it is load-bearing, and #920 carries the work of moving it."
  - "PERF.md's \"A leaf's bounds\" opened with the ink s9 reading alone. It now opens with the 2026-09-11 measurement, the three leaves it came from, and the ceiling on the grant, so the doc a person checks their memory against says why the number is where it is."
---

Raising the grant to 250,000 turns three tests red, and that is the useful half
of the reading rather than an obstacle to it: the observation window is derived
against `DefaultLeafTokens/6`, so the grant may not reach 196,608 until the
window is re-derived; a 98%-cached runaway costs about 5.8× the grant in raw
tokens, so 250,000 reaches the 1.4M the audited melt-downs reached; and the
cost/raw separation the runaway test closes on coincides at 190,000. None of
them should be moved to let a number through, so none was.
