---
kind: changed
title: A leaf's grant is 250,000 tokens, measured against real work, and it is written in one place
pr: 918
surface: [engine, chat, docs]
invalidates:
  - "A leaf's token grant was 150,000 and is now 250,000. The old figure was calibrated from a turn costing about 11k input tokens and a leaf finishing in 8 to 16 turns; measured against a real repository issue (#898, run headless on z-ai/glm-5.3, run 04c2404b26072e41) the median call carried 14.2k prompt tokens and the leaves that did the work ran 14 to 41 turns, so every one of them was landed mid-edit and the run re-planned around each landing. Anything that quotes 150,000 as the grant, or reasons about how many turns a leaf can afford, is out of date."
  - "`defaultLeafTokens` is exported as `exec.DefaultLeafTokens`. The figure used to be spelled again at three doors — `aforge exec`'s `--token-budget` default, `aforge run`'s `--token-budget` default, and `chatLeafTokens` in the chat surface, whose comment promised it mirrored the headless default. All three read the constant now, and `internal/exec`'s `TestTheLeafGrantIsSpelledOnce` fails the build on a second spelling anywhere under cmd/aforge, internal/exec or internal/session. A lane that recalibrates the grant changes one number, not four."
  - "PERF.md's \"A leaf's bounds\" opened with the ink s9 reading alone. It now opens with the 2026-09-11 recalibration and the three leaves it was measured from, so the doc a person checks their memory against carries the current figure and the evidence for it."
---

The splitting a small grant forces is not free, and that is the whole argument
for the new number. A leaf landed mid-edit costs a fresh planning round AND the
context the next leaf has to be told again, so one issue became five rounds and
seven nodes over forty-three minutes while the work itself was correct and
committed after the first two leaves. 250,000 is the smallest round grant that
covers every leaf measured, and deliberately no larger: at 400,000 every leaf
ran to exactly its ceiling, which is the lesson that produced 150,000 in the
first place.
