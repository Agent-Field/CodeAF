---
kind: fixed
title: a lane a person pinned is honoured at every door, not only in the conversation
pr: 632
surface: [engine, chat, docs]
invalidates:
  - "\"Picking a lane pins it, and every request for that model goes there\" was true of the conversation and false of everything else. `aforge do`, `aforge exec`, `aforge plan`, `aforge run`, `aforge run subharness` and the resident's own pass each sent their first request with the zero pin, because only `cmd/aforge`'s `applyV3Governance` read the `lane.<slot>` row. The sentence is now true of every door: three measured `aforge do` runs landed on the slowest of a model's seventeen endpoints with `pinned:` standing in the settings the whole time."
  - "`provider.SetLanePin` had one caller in the shipped tree and it was the chat door. The row is resolved by `config.LanePinAt` and installed by `config.InstallLaneRows`, called from `internal/config`'s `load` — beside `provider.LoadQuirks` and `calllog.Open`, which are the same kind of process-wide fact from the same profile. A door therefore no longer has to KNOW the lane row exists in order to honour it, which is why six of them did not."
  - "The three-state reading of the row — auto, `openrouter`, a machine name with its borrow flag — was written twice, in `cmd/aforge`'s `v3LanePin` and `internal/tui3`'s `laneRowChanged`. `v3LanePin` no longer exists; both callers read `config.LanePinAt`, and a test fails any file under `cmd/aforge` or `internal/tui3` that builds a `provider.LanePin` of its own. The picker's own act still goes in through `provider.RepinLane` and still forgets every refusal; only the reading is shared."
  - "The `lane.guard` row reached the chat alone for the same reason. It travels with the pin now, so a person who turned the speed guard off has turned it off for their terminal runs and their background work as well."
  - "A headless run was described in the manual as ROUTED but never as PINNED, and three chat pages said a pin chooses the provider for \"this conversation\". The lanes page now names the doors that honour a pin, and `models-and-cost.md`, `screen.md` and the settings sheet's `lane` hint say the home rather than the conversation."
  - "The `routing` row (`latency`, `price`, `off`) is NOT part of this and is still read by the chat door alone. It is a `provider.RoutingSource` on the client rather than a process-wide knob, so a headless door leaves it nil and the adapter answers per request from who is waiting. That is a default rather than a dropped instruction, and it is the same shape of gap one layer over."
---

The pin was already a process-wide knob and already had exactly the right two
entrances — a resolver's, which keeps what the wire said about a row nobody has
touched, and a person's, which forgets. What was missing was a caller: the row
was resolved at the one door that happened to be written after it existed. So
the repair is not six careful additions to six doors, which is six chances for
the seventh to be written without one. It is one line at the seam every door
already passes through to find its key and its model, and a test that fails the
day a door stops passing through it.
