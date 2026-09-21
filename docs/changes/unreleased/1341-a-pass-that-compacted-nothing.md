---
kind: fixed
title: a compaction pass that found nothing to do no longer takes the conversation away
pr: 1341
surface: [chat, engine]
invalidates:
  - A compaction pass that found nothing old enough to stub and nothing to fold
    still announced itself as a pass that had happened, and the surface handed
    its scrollback over to it. The place a person was reading was dropped to the
    compaction floor, the conversation between the two was declared already
    drawn, and the marker promising more above went out. On a session that had
    never been compacted the same pass dropped that place to the top of the
    transcript, so half a conversation on screen became unreachable by scrolling.
  - The seam was marked drawn without being drawn, so the region above a
    compaction was spliced straight onto later conversation with no line saying
    where the model's own record stopped.
  - "`session.Event` now carries `Unchanged`, which is true only on the
    EventCompacted a refused pass sends. It is absent on the wire for every
    other event and reads false on a peer built before it existed, which is the
    behaviour those peers already had."
---
EventCompacted is sent on both paths by promise: a surface opens a row on
EventCompacting and has to be able to settle it whether the pass edited anything
or not. One value therefore carried two meanings, and the failing one was the
silent one, so a reader could not tell a transcript that had been replaced from
one that had not been touched. The field is the disjoint range, and its zero
value is the meaning that was always safe.
