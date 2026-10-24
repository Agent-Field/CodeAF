---
kind: fixed
title: Automatic credit recovery honors free-route consent
pr: 1905
surface: [chat, engine]
invalidates:
  - "Low credit and payment refusals previously allowed automatic free routes even with the switch off. Every automatic default, recovery ladder and helper now honors opt-out."
  - "A low-balance record previously blocked paid routes before an actual payment refusal. With free routes off, a small positive paid balance remains usable."
  - "Resolved helper models previously counted as explicit consent. Helpers now require current explicit settings, and malformed settings fail closed."
---

A current explicit choice of a named free model permits that model without
enabling other automatic free fallbacks. When no allowed route remains, work
is preserved and the user is guided to add credit, connect a provider, or
explicitly enable free routes. Saved preferences and pins are not rewritten.
