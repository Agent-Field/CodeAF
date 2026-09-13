---
kind: added
title: a fourth routing mode, `simple`, sends exactly what you asked for and nothing else
pr: 1016
surface: [engine, chat, docs]
invalidates:
  - "The `routing` row had three answers — `latency`, `price`, `off` — and the settings hint, the lanes page and the Providers-tab list all said so. It has four: `simple` sits between `price` and `off` in the cycle, and with no lane pinned the request carries NO provider object at all, so OpenRouter's own default routing answers and aforge does no measuring, no hedging and no re-ranking of its own."
  - "`off` was the only way to send no preference, and it costs the lane sheet, the pin rows and the speed guard because nothing is measured. `simple` keeps the pin: a lane a person named goes out as a strict demand (`only`, fallbacks off) with nothing else on the request, and a pin written `borrow when slow` collapses to strict because no rescue runs for it to borrow."
  - "The routing row was believed to reach every request this process makes. It did not: `internal/session` handed its own clients the answer and every other client in the binary — the harness, the subharness, `read_document`, `view_image`, a `/model` panel's members — was assembled through `config.Config.ClientConfig`, which carried no routing answer at all, so each of them ran `latency` whatever a person had written. Under `simple` that was silent and costly: a client on the ranked road may stand a strict pin down on the SAVED account-exclusion belief before any wire is asked, the stand-down is process-wide, and the conversation's own next turn then went out bare while the status line still read `@deepseek`. `config.InstallLaneRows` now installs the row beside the lane rows (`provider.InstallRouting`), and a client handed nothing answers it; a client handed an answer still keeps its own."
  - "A strict pin on a machine `~/.aforge/v3/account-exclusions.json` covers was stood down before the call in every mode. Under `simple` it is not: the pin goes on the wire once, OpenRouter refuses it, and the pin is retired through the one existing door — the `X cannot serve this model; routing on auto for this model until you pin again` note in the conversation, and `@X` coming off the model word in the same breath. One refused round trip a window is what the row's promise costs."
  - "The lanes page's machinery — the takeover, the closed set, the refusal walk, the probe — read as what aforge does while it is choosing. It is what `latency` and `price` do; under `simple` the routing gate, the belief chooser, the hedge and the probes stay compiled in but are disconnected from the request path, and none of it runs."
---

The provider-choosing stack is not deleted, it is disconnected: every layer still
compiles and still answers the other three modes, and under `simple` the row a person
wrote is the whole algorithm — no pin means no provider object and the router's own
default, a pin means exactly that machine. Pin retirement on a terminal refusal is
unchanged, and pinning again after one puts the pin straight back.
