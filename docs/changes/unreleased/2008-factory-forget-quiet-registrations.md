---
pr: 2008
kind: fixed
title: Forget a computer's quiet registrations and dead local factory addresses
surface: [engine]
invalidates:
  - "Was true: every attach of a runner added a registration and none was ever removed, so a computer whose factory changed address on each start collected hundreds. Is true: when the same person's same computer attaches again, its registrations silent past the runner timeout and holding no job are forgotten."
  - "Was true: the runner's worker.json kept a credential set for every address its own factory ever listened on. Is true: saving a credential for a factory on this machine forgets the other loopback addresses; a factory elsewhere is kept."
---

A factory that `codeaf app` runs listens on a new loopback port each time it
starts, so its runner attaches again with one new credential per slot, and
the old ones stayed in the registry forever. Only that person's own computer
is cleaned, a registration a lease names is always kept, and a runner whose
quiet credential was forgotten is told it is not attached and attaches again.

The runner's own credential file had the same growth: one entry per loopback
port its factory ever used. Saving a credential for a local factory now drops
the other loopback addresses, which can no longer answer.
