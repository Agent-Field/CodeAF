---
kind: fixed
title: The race can no longer pull the first taking-stock mark below half the handoff price
pr: 1015
surface: [chat, engine]
invalidates:
  - "A raced both-yes from the pre-turn route judge pulled the checkpoint's first mark down to the very step boundary the verdict landed on (`checkpointMeter.tighten` set `firstAt = rounds + 1`). Measured on a real planning turn (ded9ace743f58581, 2026-09-12): the verdict landed two rounds in, the first `[taking stock]` fired at round three over one issue and two listings — 43 KB of mostly listing noise — the model waved the note past, and the round-twenty note inherited that worthlessness. `firstAt` is now clamped at `checkpointFirstRungFloor`, derived as `checkpointPrice / checkpointRatio` (round 5 today): early enough to catch a runaway, late enough that the note's rounds, files and bytes are real. An unraced turn's ladder is byte-identical — rungs at 10, 20, 40."
---

The tightening itself is untouched in purpose: it is triage, the power the race
kept when the benchmark took conversion away from it — look sooner, never
oftener. Nothing in the measurement that introduced it (the mark moving to the
verdict's own boundary) spoke to how early a note is worth sending, so the floor
follows the only evidence there is, the failure above: half the price, derived
from the two constants it is a compromise between rather than written as a
number of its own.

The manual's screen page needed no change: it describes the ordinary ladder
("after ten finished rounds, and again after twenty"), which is exactly what an
unraced turn still climbs, and it never documented the race's exception that
this bounds.
