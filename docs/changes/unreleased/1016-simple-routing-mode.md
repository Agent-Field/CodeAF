---
kind: added
title: a fourth routing mode, `simple`, sends exactly what you asked for and nothing else
pr: 1016
surface: [engine, chat, docs]
invalidates:
  - "The `routing` row had three answers — `latency`, `price`, `off` — and the settings hint, the lanes page and the Providers-tab list all said so. It has four: `simple` sits between `price` and `off` in the cycle, and with no lane pinned the request carries NO provider object at all, so OpenRouter's own default routing answers and aforge does no measuring, no hedging and no re-ranking of its own."
  - "`off` was the only way to send no preference, and it costs the lane sheet, the pin rows and the speed guard because nothing is measured. `simple` keeps the pin: a lane a person named goes out as a strict demand (`only`, fallbacks off) with nothing else on the request, and a pin written `borrow when slow` collapses to strict because no rescue runs for it to borrow."
  - "The lanes page's machinery — the takeover, the closed set, the refusal walk, the probe — read as what aforge does while it is choosing. It is what `latency` and `price` do; under `simple` the routing gate, the belief chooser, the hedge and the probes stay compiled in but are disconnected from the request path, and none of it runs."
---

The provider-choosing stack is not deleted, it is disconnected: every layer still
compiles and still answers the other three modes, and under `simple` the row a person
wrote is the whole algorithm — no pin means no provider object and the router's own
default, a pin means exactly that machine. Pin retirement on a terminal refusal is
unchanged, and pinning again after one puts the pin straight back.
