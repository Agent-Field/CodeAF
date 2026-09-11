---
kind: internal
title: Keep the pulse allowance fixture inside one local calendar day
pr: 653
surface: [chat]
invalidates:
  - "The pulse allowance fixture inherited the wall clock and could date its spending outside today's local ledger window. It now runs from an explicit local-noon clock."
---

The formatter acceptance now uses an explicit local-noon clock, so a CI run just
after midnight cannot date its non-zero spending row on the previous day.
