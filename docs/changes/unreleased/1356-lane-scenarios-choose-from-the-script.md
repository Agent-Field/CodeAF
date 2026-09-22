---
kind: fixed
title: the router scenarios choose from the script, so a busy box cannot decide whether the router steers
pr: 1356
surface: [engine]
invalidates:
  - "`internal/lane`'s scenario 2 was judged on scripted first tokens, and the
    belief it routed on was still the wall clock. Both ends are on the script
    now; only the logged wall figures and the instrument scenario read a timed
    one."
  - "A red on TestS2TheDefaultGoesSlowAndTheRouterMoves reading `0.0% better`
    says one thing: at least three of the routed arm's twenty asks went to the
    broken lane after the break. The p90 over twenty answers is quantized, so
    two broken answers score about 79% and three score exactly 0.0%; the number
    is a cliff and not a margin, and it is not a flake to rerun."
  - "`lanestub.Server` now answers `Script(model, lane)` with the profile a lane
    is serving under right now, so a scorer reads the script back instead of
    keeping a copy that goes stale when a scenario re-scripts a lane."
---
The wire in these scenarios runs a hundred times faster than the world it
describes, so a millisecond between a token landing on the socket and the reader
waking up to stamp it is a hundred milliseconds of believed first token. On a
quiet box that is a quarter of a second of world time and harmless. Beside a
full suite it was measured between one and ten seconds, and it lands on the
healthy lanes and the broken one alike: a scripted 0.8 s against a scripted 4 s
is five to one, and the same pair with 1.5 s added to both is barely two to one,
because an additive error does not cancel in a ratio. The chooser then stops
telling a lane that has gone bad from the lanes that have not, the arm that is
supposed to move goes back to the broken lane, and the scenario reports that the
design does not work.

The gate, the thirty per cent, and the clause that the moving arm must beat at
its tail what the pinned arm has at its middle are all unchanged. What changed
is where the router's knowledge comes from while it is being tested.
