---
kind: fixed
title: The wall refuses a first replan round it cannot hold
pr: 1144
surface: [resident]
invalidates:
  - "`growJob` in `internal/resident/grow.go` never refused a round with `CauseOutOfWall` for a job with no measured pace: `jobPace` answered zero and zero read as \"not near\", so a first replan round was admitted at 40m22s of a 45m wall (ds1, awilix), ran long, was killed by the clock mid-round, and the job was released with its root unlanded and no delivery gate ever cut. A job with no measured pace is now read against the elapsed runtime of the leaf that just overran and refused, through the existing `RefusedOutOfWall` close-out, when the wall cannot hold even that."
---

A round the wall cannot hold is now handed over rather than killed mid-flight,
through the same `closeOutJob` path every other out-of-wall refusal takes, so a
gate precedes the wall on the first replan round too. The refusal turns only on
evidence: a measured pace, or an overrun leaf whose elapsed life the wall cannot
fit. A job with nothing run and nothing measured has no estimate at all, so its
genuine first round is admitted rather than refused — refusing a round that
cannot be costed produces nothing, and it would take every do and headless run
(each under a wall shorter than any fixed floor) down with it. The measured-pace
branch is unchanged.
