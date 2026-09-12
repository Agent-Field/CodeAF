---
kind: fixed
title: A hedge scenario whose controller says nothing is failed by the scenario, not by the test clock
pr: 983
surface: []
invalidates:
  - "A HOLD ON `theControllersWord` IS NOW BOUNDED, and by the scenario rather
    than by the package. `internal/provider/hedge_test.go`'s hold used to be
    closed by the control ladder and by nothing else, so a controller that never
    spoke left the lane holding its first word until the test binary's own
    deadline and the reader got a fifteen-minute timeout naming whichever test
    happened to be running. `laneRig.patience` — the one place a scenario states
    its ceiling — now arms the hold for that ceiling times `heldWordSlack`, and
    on expiry the hold opens and the scenario fails on one line naming the
    silence, the bound and the last rung of the waiting ladder a person was told
    about. A scenario that asks for the word and states no patience is told so at
    cleanup instead of hanging."
---

A controller that says nothing was the one silence this fixture could not name.
Every other wait in it ends on a signal; this one ended on the clock, and a clock
reports the test it interrupted rather than the claim that broke. The bound comes
from the ceiling the scenario already states, so there is no second figure to
drift, and the multiple is slack for the wire — the controller acts AT the
ceiling and its rung still has to cross the reader that closes the hold — and not
a second ceiling.

The verdict is said from `t.Cleanup`, after the watcher goroutine has been
joined, because a `t.Errorf` from a goroutine that has outlived its test is a
panic and not a verdict.
