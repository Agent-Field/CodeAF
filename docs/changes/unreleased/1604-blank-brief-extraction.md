---
kind: fixed
title: blank terminal briefs stop before planning or model work
pr: 1604
surface: [cli]
invalidates:
  - "An empty quoted brief could previously start model work with no goal. Empty and whitespace-only briefs are now rejected at the shared text-input boundary before planning, execution, or saved-plan creation."
---

Focused extraction for issue #1566. No other broad audit behavior is included.
