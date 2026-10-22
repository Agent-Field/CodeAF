---
kind: fixed
title: Direct services stop before OpenRouter receipt lookups
pr: 1894
surface: [engine]
invalidates:
  - "A direct service's late receipt lookup previously followed an OpenRouter attempt base with the service's own key. Direct services now stop before any receipt lookup, regardless of the attempt base."
---

No shipped service routes overflow to OpenRouter. The shipped overflow doors
belong to the same vendor as the configured service. Direct-service usage
billing and OpenRouter's own late receipt recovery are unchanged.
