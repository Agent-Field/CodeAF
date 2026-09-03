---
kind: fixed
title: a usage frame is folded into the call's count, never swapped in for it
pr: 405
surface: [engine]
invalidates:
  - "In the streamed read loop of `internal/provider/client.go`, a chunk carrying a usage object replaced `response.Usage` and the reasoning count outright, so a provider that reported token counts in one frame and cost in a later one ended the call with the counts and the reasoning count zeroed. Every usage frame is now folded in through one helper, `usageWire.mergeInto` over `mergeUsage`: a figure the frame states wins, a figure it is silent about keeps what an earlier frame carried, and a call that never saw a usage frame still ends with a nil usage (unknown, never zero)."
  - "The whole-body completion, the streamed chat loop and the music stream (`music.go`) each read a usage block their own way. All three go through `mergeUsage` now, so a second reader cannot drift from the first."
---

Latent rather than observed: DeepSeek and Kimi both send one terminal usage frame,
so nothing was zeroed in the wild. That is a property of today's endpoints and
not of the protocol, and the ledger bills what the wire reported in whatever
order it arrived.
