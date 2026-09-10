---
kind: fixed
title: internal/config builds every provider client, and a thinking level leaves the model id
pr: 757
surface: [chat, engine]
invalidates:
  - "`Config.providerConfig` was the one place a profile became a `provider.Config`, and it was unexported with five in-package callers. It is gone; the doors are `ClientConfigFor`, `Config.ClientConfig` and `WithoutSeatPin`, all exported, and a call outside internal/config that sets `APIKey` or `BaseURL` on a `provider.Config` now fails a law on `make test-laws`."
  - "A harness, a subharness, a conversation, a standing pass, a memory consolidation and a document read each copied the key and the base URL into a `provider.Config` of their own. All six now ask internal/config, so a change to how a client is built reaches every one of them instead of five of six."
  - "A model id carrying a thinking level — `slug:low`, what a tier row and `--plan-model` produce — went on the wire whole from those sites, which is a slug no provider publishes. The level is now split off everywhere and travels as the request's reasoning field; a session launched on `slug:low` reports its model as `slug`, so a status row that used to show the suffix no longer does."
  - "A standing pass and a memory consolidation sent no `provider.max_price` and no parameter gate, because their clients were built without the catalog seams. Both now carry what the session publishes, so a latency-sorted ask from either is bounded the way every other request on the machine is."
---

The two live consequences are the level and the ceiling; the standing defect is that
`internal/config` learning a second base URL or a second key would have reached none of
those six. Behaviour for a plain model id with no catalog behind it is unchanged, and a
test at each door asserts that.
