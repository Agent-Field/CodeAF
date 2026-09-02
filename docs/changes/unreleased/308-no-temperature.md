---
kind: changed
title: No call sets a temperature — every request leaves sampling to the provider
pr: 308
surface: [engine, chat]
invalidates:
  - "Every request carried `temperature: 0.2` — a base default in config, sent on the wire by the provider adapter on every call. No call sets a sampling parameter now; the field is absent from the request and the provider's own default answers, exactly as OpenRouter documents for an omitted parameter."
  - "The reflex pair, the route judge, the division reviewer, the checkpoint's reads, the adaptive planner, the task judge, namer, shaper and spell-out, and the memory tidy each pinned their own temperature (0, 0.2, 0.3). They set nothing now."
  - "`harness-design -temp` tuned the design turn's sampling. The flag is gone; the rig's requests omit sampling parameters like everything else."
---

OpenRouter omits an absent sampling parameter upstream rather than substituting
one, so a temperature on the wire was a decision this program was making about
somebody else's model — and it was being made in a dozen places, each with its
own number and its own rationale. All of them are gone. The SDK boundary (a
plain OpenAI-compatible endpoint, where the SDK's own loop injects its
configured temperature per round) is covered by an option that takes the field
back off after any caller's options, so the promise holds there too.
