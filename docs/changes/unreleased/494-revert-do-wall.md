---
kind: internal
title: the do wall as a duration (#379) is off dev until it re-lands in the shape the leaf-room law allows
pr: 494
surface: [engine]
invalidates:
  - "#379 said `aforge do -timeout` takes a duration with a unit. It is not on dev: its default was spelled `15 * time.Minute`, the exact spelling internal/exec reserves for the subharness table's leaf deadline, and the structural test caught it. `-timeout` is an integer of seconds again until the re-land, which keeps the unit parsing and spells the same fifteen minutes as `900 * time.Second`."
---
