---
kind: fixed
title: a leaf knows its own clock, and reading forever stopped counting as progress
pr: 610
surface: [engine, docs]
invalidates:
  - "The no-progress guard had three signals — repeated identical calls, a stagnant window, and a 60-turn floor — and none of them could see a leaf that only reads. It has a fourth: consecutive turns that carried tool calls and left the workspace unchanged, counted independently of whether the results were new. `noProgressReconTurns` is 10 and `noProgressReconConclude` is derived from it; the first span asks the leaf for its result, the second concludes it through the existing conclude-grace-terminate ladder. A mutation of any kind, including a shell command that leaves a file behind, clears the count."
  - "A result the leaf had never seen before was progress without bound, so 156 distinct greps in one measured run were 156 distinct content hashes and `stagnantTurns` never left zero across a whole 45-minute wall. New information still resets the stagnant window exactly as before; it no longer resets the mutation-free count, which is the signal that separates a leaf gathering from a leaf producing."
  - "The only thing a leaf was ever told about its wall arrived once `time.Until(deadline) <= deadlineLandingReserve`, a tenth of the wall capped at two minutes, and in one measured run it never arrived at all. A leaf's brief now names the wall it has, spelled the way `--timeout` accepts it, and `wallPaceAt` (0.5) buys one live reading of what has gone and what is left, injected after the deadline landing check so a leaf already landing is never paced."
  - "`RoomLeft` computed a leaf's remaining wall for the self-close pass and nothing surfaced it to the model as a budget it could pace against. It still does exactly that; the leaf's own reading is a separate once-only message from the turn loop, and both it and the recon notice write a `trace.note` beside the existing conclude line, so a run's record names them."
  - "PERF.md's table of a leaf's bounds listed five ceilings and every one of them was a magnitude. It now also carries the mutation-free recon pace and escalation and the wall reading, with the escalation marked as the only one of the three that may land a leaf — and it lands it by concluding, never by killing it outright."
  - "Nothing in the chat manual answered someone asking why a run spent its whole time reading and produced nothing. `internal/manual/chat/adaptive-runs.md` now says what the worker is told about its clock and when, and states the refusal plainly: reading is never by itself a reason a worker is stopped, and a worker whose result is the answer itself is asked for the answer rather than killed for not writing a file."
---

The load this has to hold under is a model that read for forty minutes with
verified line numbers in its prompt and an explicit instruction to start
writing. Advisory text did not move it, which is why the second span concludes
rather than asking a third time — but it concludes into the landing every other
signal already uses, so a leaf whose deliverable really is its own reply is
asked for that reply instead of being thrown away.
