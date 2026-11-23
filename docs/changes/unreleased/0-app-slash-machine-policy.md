---
kind: added
title: Desktop wire doors for cache, autonomy and crew policy
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: cache, autonomy, saved harnesses and the persistent crew rule had no typed desktop door. Is true now: cache.status/clean, autonomy.get/set, harness.list and settings.crew.get/set share the terminal implementations; harness selection remains a client draft chip. Existing settings.models.set pins and settings.set task caps are reused."
---

Cache deletion requires explicit confirmation. Project autonomy preserves the two
always-ask floors and follows the conversation's host handle. Machine methods stay
local. Crew dynamic model rules and daily caps have their own policy door because
the existing model ticks and machine spending limit are different settings.
