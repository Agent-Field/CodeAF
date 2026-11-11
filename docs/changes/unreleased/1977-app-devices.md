---
kind: added
title: Desktop engines can list, rename and remove paired computers
pr: 1977
surface: [engine]
invalidates:
  - "The app engine had no device-management methods. Engines that advertise devices.manage now serve devices.list, devices.rename and devices.remove."
  - "The app roster previously supplied removal copy only after removal. Removable rows now carry the engine's target-specific remove_line for confirmation."
  - "An older concurrent roster read could restore a removed computer's active state. Completed removals now survive older responses and failed refreshes."
  - "The Devices observer previously kept the old pairing's roster and refusal until restart. It now resets when the relay, identity or device changes and ignores old clients' callbacks."
---

The app wire lists this computer first and includes other computers paired to
the same identity, their platform, removal state, last sighting and known
presence. Unknown presence is omitted. Only this computer can be renamed; another
computer can be removed by its id. Receipts use the engine's existing rename and
removal sentences, including the limits of removing a lost computer.
Removable rows also include the optional `remove_line`, using that same
target-specific engine copy before confirmation; self and removed rows omit it.

Never-paired homes list only this computer and explain how to add another with
`codeaf pair` from a terminal. Sync-off and unavailable homes also answer locally.
These reads make no relay request. Device management starts no polling or watch
socket: an explicit list reads the roster and presence, while `devices.changed`
reuses facts received by existing engine requests and sockets. Rename and remove
emit a snapshot without a follow-up relay read, preserving a completed change
when a later refresh fails. Pairing itself and the desktop Settings UI are separate.

An older concurrent roster response cannot undo a completed removal. Equal or
unversioned roster reads retain removed records until a newer directory version
says otherwise.

After terminal re-pairing, the running app engine drops the old
roster, version, presence and refusal on its next snapshot. Late responses and
watch frames from the former relay, identity or device cannot restore those facts.
