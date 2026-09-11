---
kind: added
title: a chooser change is judged by regret on ten days of log, not by a screenshot
pr: 922
surface: [engine, docs]
invalidates:
  - "The way to replay the call log against the chooser was `internal/lane/replay_bench_test.go` behind `go test -tags replay -run TestReplayLog ./internal/lane/` with `REPLAY_LOG`, `REPLAY_SINCE`, `REPLAY_FACTS` and `REPLAY_TASK_LAMBDA0`. That file is DELETED. The instrument is `cmd/aforge-replay` behind `make replay [LOG=… SIGHTINGS=… SINCE=… WINDOW=… OUT=…]`, it has a fixture and a law test, and docs/design/recovery/DESIGN.md §8 is where a chooser change is now required to cite it."
  - "The figures in `docs/design/routing/ASSESSMENT-20260911.md` §Replay were produced by that bench at a λ of 90 everywhere, scoring each variant on its own set of requests and pricing the machine that served partly from the very answer being scored. They are not comparable line for line with `make replay`, which prices per ROLE, scores every candidate on one common set, leaves a request's own answer out, and counts what is censored. The doc says so now."
  - "It was believed that giving internal/lane's Kalman filter process noise — a floor under how certain it may become about a machine's median — would have avoided the long Morph answers of 2026-09-10/11. Replayed over ten days with the floor measured from how far a machine's own days differ (0.2409 nats²), `current+Q` is a hair WORSE than `current` on both role classes (8.62s against 8.06s mean watched regret) and switches machine more often. The floor buys nothing on this evidence."
  - "It was believed the shipped chooser and the router mostly agree. Over the same ten days the chooser's own demand is the machine that actually served only about a quarter of the time (25% watched, 25% unattended) while the machine the preference NAMED served 93%. The gap is `allow_fallbacks` (#850), and it is now a measured number rather than an argument."
  - "`cmd/aforge-census` owned the only reader of `calls.jsonl`. The reader is `internal/callrows` now — one `Read`, one `Row`, and the four facts every instrument needs from a line (finished, failed, a hedge's losing arm, which ceiling applied). The census keeps its own judgement about what those rows MEAN and takes the decoding from there."
---

The chooser is a bandit and we were tuning it by anecdote: #850, #853 and #873 were
each argued from one screenshot and a ten-row grep. The log already holds about a
thousand finished requests a day with everything a replay needs, so the argument now
has an instrument, and the first thing that instrument prints is its own error.
