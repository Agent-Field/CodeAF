---
kind: fixed
title: the routed memory block rides at the tail, so message[0] holds still for a session
pr: 812
surface: [chat, engine, docs]
invalidates:
  - "`message[0]` was `system + places + standing + memory + record`. The `<memory>` block is no longer in it: it lands at the tail of the transcript as its own appended note, beside the state card and the `<elsewhere>` block an earlier wave moved there. `refreshSystemLocked` renders four blocks now, not five."
  - "A test said memory stays in `message[0]` because it 'holds for the life of the conversation'. It does not — it is re-routed against the person's words at the start of every turn and its age labels re-stamp hourly — and that was costing a working session its whole cached prefix on any turn the subject moved."
  - "A task node's memory brief used to be readable in `child.messages[0]` the moment the node was built. It is in the transcript now, and lands on the drain immediately before the node's first request."
  - "BENCH.md §1a's line that the diet's cached share 'fell 40.6%' reads as a regression and is not one. §1c is the autopsy: nothing on either branch moved a prefix, and the median request's own cached share ROSE 83.0% → 95.2%. The falling figure was a ratio of two medians standing in for the median of a ratio."
  - "`bench/prompt-diet/` has a fourth script. `prefixdiff.py` reads a run's request bodies and reports, per request, how many leading bytes it shared with the one before it and what broke the sharing — the cause `compare.py` cannot see, because a cached share is an outcome."
---

DESIGN.md §0's first priority is never busting the cached prefix, and until this
change one block in `message[0]` broke it on a beat nothing else there moves on.
A folder attached, an order stood up, a question answered: each is a thing a
person did, at most a handful of times in a session. The `<memory>` block is
chosen afresh by a router against the person's own words at the start of EVERY
turn, and `renderMemoryBlock` stamps each line it keeps with an age label whose
granularity is hourly for anything learned today — so it could move on a turn
where the router had chosen identically, and `message[0]` sits in front of every
message there is.

It rides in a note of its own rather than the card's because the two move on
different beats, and one note would re-send up to 4,800 runes of memory every
time a goal changed. What the move costs is a superseded line left standing in
the transcript where it was said, which is why the note's opening says the last
one holds — the same sentence, for the same reason, as the note the card rides in.
