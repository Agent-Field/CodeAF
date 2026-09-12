---
kind: fixed
title: a running stream teaches the ledger mid-stream, so the next pick is not working from this morning
pr: 964
surface: [engine]
invalidates:
  - "Every `lane.Sighting` was built at settlement — `Client.noteVelocity` from the stream epilogue, and `streamWatch.sighting` for a losing arm — so while one endpoint spent minutes writing, the belief the chooser ranks on still described that machine's morning. The 14:32 retry of 2026-09-11 picked the same collapsed Morph endpoint the 14:29 call was at that moment still being throttled by. A live stream now teaches the ledger as it runs: the read loop folds a partial sighting in at most once a minute, each window covering only the span since the one before it, and the settlement sighting is rebased to cover only the tail no partial claimed — so one stream is one measurement told in chapters, never the same span twice."
---

This is the mid-stream teaching seam of #891 §1 that #924 measured and
deliberately did not take (it took the live-wire rescue and the
watch-every-attempt half). `streamWatch` already held the live figures —
`tokens`, `first`, `last`, `gap` — to drive the hazard controller; nothing
folded a partial reading into `lane.Ledger` until the call settled, so
`beyondThePatience` → `choice.Ignore` → `provider.ignore` read a belief that
had not been told. The windows are non-overlapping because successive readings
of one stream are not independent observations, which is why the change sits at
the `streamWatch`/`sighting` seam and not at a call site.
