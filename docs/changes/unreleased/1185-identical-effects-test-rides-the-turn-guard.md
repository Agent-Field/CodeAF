---
kind: fixed
title: the identical-effects test is made deterministic — no clock, no load, no turn-guard race
pr: 1185
surface: [chat]
invalidates:
  - "`TestARunOfIdenticalEffectsEndsNothingByItself` was believed to flake only under load. It flaked without any: forty identical `write` calls make the child turn's own repetition guard (looped.go) hand the turn over at its seventh write, and whether the run's drain reached its second repeat checkpoint before that turn closed was already a race — `len(rounds)` came back 2 or 3 across 250 runs of the untouched test. The `>= 2` assertion sat on that boundary."
---

The test rode two real-time dependencies, neither of them the leash threshold it
exists to guard (the deadline branch cannot fire: the run's allowance is 60
minutes and this run lasted 0.21s). It now spells its forty saves differently
while leaving the same effect, so the turn's repetition guard — which keys on the
call — never fires, and it saves an empty file, so the leash's fingerprint of that
file cannot race the next truncating write. The assertion now holds the behaviour
`effects.pardon` exists for: the reader is asked once per run of identical effects
and never on every step, which it fails on when that reset is removed.
