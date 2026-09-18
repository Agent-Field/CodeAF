---
kind: fixed
title: The wall refuses a first replan round it cannot hold
pr: 1144
surface: [resident]
invalidates:
  - "`growJob` in `internal/resident/grow.go` never refused a round with `CauseOutOfWall` for a job with no measured pace: `jobPace` answered zero and zero read as \"not near\", so a first overrun round was admitted at 40m22s of a 45m wall (ds1, awilix), ran long, was killed by the clock mid-round, and the job was released with its root unlanded and no delivery gate ever cut. A job with no measured pace is now read against a conservative floor — the elapsed runtime of the leaf that just overran, or `noPaceRoundFloor` (four minutes) where its start was never stamped — and refused, through the existing `RefusedOutOfWall` close-out, when the wall cannot hold even that."
---

The measured-pace branch is unchanged, and so is the bias: the floor is the
least a round of this kind is assumed to cost, so a round the wall can fit is
still admitted and a job is handed over only when the clock clearly cannot
hold one more. The refusal goes through the same `closeOutJob` path every other
out-of-wall refusal takes, so a gate now precedes the wall on the first round
too.
