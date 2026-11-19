---
kind: fixed
title: Asking for four pictures on a model that draws one per request makes four instead of failing
pr: 2845
surface: [chat, engine, app]
invalidates:
  - "generate_image's n was sent as ONE request, and a model that could not return several failed the call with the router's raw 400 and retried nothing; now a model whose catalog row publishes a smaller n maximum (every Gemini image model publishes 1) is asked in several requests at once, and with no published maximum a refusal about the count alone is asked again once as single pictures."
  - "A generate_image call with n above 1 was always one provider request with one call-wide cost; now it may be several requests, each picture from a single-picture request carries its own cost, and the render record's charge is the sum of every request (unknown if any one said nothing)."
---

The desk's Make four on google/gemini-3.1-flash-image showed a person the router's
"n: must be exactly 1" rejection. Partial success keeps the pictures that came back
and names each request that did not. A refusal about anything other than the count
is still reported as it came and never retried.
