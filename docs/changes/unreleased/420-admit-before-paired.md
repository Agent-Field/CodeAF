---
kind: fixed
title: the machine writes a device into its list before it says the pairing held
pr: 420
surface: [remote, docs]
invalidates:
  - "Two internal/pair tests failing under a loaded box — TestPairingThenAWholeConversationThroughARelayThatCannotReadIt and TestARevokedDeviceIsTurnedAwayByTheMachineItself — were read as a load flake worth waiting out. They were not: pairAsMachine said \"paired\" and only then wrote the device down, so a first `aforge chat --at` on a slow disk could be told in the next breath that the device had been stopped. The admit now happens before the reply."
  - "The stopped-device sentence — \"this device has been stopped on that machine — pair it again from there\" — meant either a revoked device or a pairing whose write had not landed yet. It now means only the first: a first pairing never says paired and then stopped."
  - "A pairing the machine could not write down used to be announced to the device as a pairing that held, leaving a device that believed it was a key to a machine with no record of it. The machine now hangs up instead, and the device is told what a wrong code is told; the reason — which write failed — stays on the screen of the machine that owns the disk, as `could not write down that pairing: …`."
---

`pairAsMachine` sent the last message of the introduction — the one the surface
reads as "paired" — and returned; only then did `Host.pair` call `Devices.Admit`,
a directory create, a write and a rename. The surface re-dials on that reply at
once and each stream is answered on its own goroutine, so the connect's
`Devices.Allows` could run against a book that had not been written yet, find
nothing, and answer with the sentence for a device somebody deliberately revoked
— sending the person looking for a revocation that never happened.

The admit is now asked inside the exchange, ahead of the reply, which is the
shape `acceptAsMachine` already had. Nothing on the wire changed: the last
pairing message is the machine's key and its name, as in every build, and a
machine that cannot write the pairing down refuses it by hanging up. Across the
issue's ten parallel test binaries, forty runs each, the two witnesses went from
15 and 29 failures in 400 to none.
