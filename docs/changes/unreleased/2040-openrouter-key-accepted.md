---
kind: fixed
title: Setup no longer says a dead OpenRouter key works
pr: 2040
surface: [engine]
invalidates:
  - "keys.check proved an OpenRouter key with one request, the key's model listing (/models/user). That listing answers 200 with the public catalog for any bearer, so a revoked or made-up key read as \"Key works\" and failed its first turn with 401. keys.check now asks OpenRouter's key endpoint (/key, or /auth/key where /key is not there) first, and lists models only after it accepted the key."
---

OpenRouter's `/key` answers only for a bearer it resolved to a live key: its
200 carries that key's own row (label, limit, usage), and an unknown or revoked
key is a 401 such as "User not found.". A refusal there is answered with that
status and the provider's own words before any model is listed. A key row that
is not a JSON `data` object, or that marks a management key (which OpenRouter
lets manage keys but never run a model), is refused in codeaf's own words. No
request follows a redirect with the key on it, and all of them share one
ten-second deadline. A key whose
credit or spending limit has run out is still accepted here, as before: that is
the balance's to say. Every other provider's check is unchanged.
