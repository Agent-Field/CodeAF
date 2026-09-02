---
kind: fixed
title: the headless doors fetch a lane sheet of their own
pr: 354
surface: [engine, resident]
invalidates:
  - "The lane-sheet beat was a session's errand and lived on `session.Agent` alone. It is also the process's: `cmd/aforge`'s `startLaneBeat` (lanebeat.go) is seated at `installMeasuredRulers`, so `do`, `run` and `run subharness` — none of which build a session — fetch an endpoints page too."
  - "`aforge do` left `$AFORGE_HOME/v3/lanes/` empty and a ledger holding the one lane each run was served by. It leaves a sheet cache per model and a belief per lane with `Facts.Known()` true, so the chooser ranks a frontier on a headless-only install."
  - "`lane.Beat` ran a fetch loop per caller. It admits ONE BEAT PER SHEET: a second call hands its models to the running beat through the queue it already had and returns at once, and a model the round already holds is not re-fetched when it arrives from that queue."
  - "A session was always the one beating. Where a process beat is already running, `Agent.startLaneBeat` becomes a join — its code, its refusals and its context are unchanged."
---

The lane ledger is one object every surface feeds and reads, and until now the
fetching half of it belonged to one of them. A headless install therefore wrote
sightings forever and never read a sheet, which is the "chooses between no lanes
at all" condition resurrected for one door (#318).

The beat is now the process's, at the one function every surface that records
anything already calls, on a lifetime cancelled at the one exit every command
shares — beside the belief writer that runs for exactly as long.
