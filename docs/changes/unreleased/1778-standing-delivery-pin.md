---
kind: changed
title: standing deliveries are durable-first and a ratified item keeps its model policy
pr: 1778
surface: [chat, engine]
invalidates:
  - "A firing that reached an open window wrote no inbox note. The note is now made durable before the live offer, so a crash or a window closing between the offer and the reading cannot lose the line."
  - "A file watch took its baseline at the first wake. Ratification now arms it at the yes, and a scan that could not read everything it matches sets no baseline and leaves a visible needs-person flag."
  - "An ambiguous, refused or timed-out judgment was folded into a decided no. The real tick now carries the three-way sentinel: an undecided look writes nothing and the item stays due."
  - "A background pass reloaded the profile's tiers, role pins and fallback ladder, so an item ratified under `--one-model` could be answered by a different model. The item freezes the ratifying conversation's model policy on its origin and the pass honours it for the sentinel and every child seat."
---

The deferred-delivery core was already in place; this change is the session and
CLI half that made it real end to end.

A delivery is appended to its inbox under the pending identity and Sync'd before
anything is drawn or queued, then offered to an open window on top of the note.
The inbox dedups on the identity across retries and restarts. The live handoff is
at least once: a line offered live and then folded when the window is reopened can
be read twice, because whether a screen drew the row is not observable here, and
loss is the direction this refuses to fail toward. A write that could not be made
durable, or an item with no address at all, is reported so the caller keeps the
intent rather than reading success.

A ratified FILE watch is armed at the yes, so a change between the card and the
first wake is a change. A bounded scan that could not read everything it matches
sets no baseline, leaves a visible flag, and the first complete look clears it.

A ratified item also keeps the model policy it was given on: when the session ran
under `--one-model`, the origin records the promise and the model, the origin is
written at the schema barrier so a build without those fields skips it, and the
reloaded background pass resolves the sentinel and every child text seat from the
item's pin with the role pins, tiers, fallback chain and nearest-model guess
withheld.

One behaviour changed for the keyless walk: an undecided look (for example, a
machine with no API key) is counted as checked and writes nothing, rather than
being published as a failed check. The item stays due and is retried next pass.
