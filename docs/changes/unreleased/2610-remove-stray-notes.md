---
kind: fixed
title: a working-notes file committed by mistake is removed from the repository root
pr: 2610
surface: [build]
invalidates:
  - "Was true: NOTES-FIX-2597.md sat at the repository root, a scratch record of one fix round's local test runs. Is true now: it is gone; nothing in the build, the tests or the manual read it."
---

The file arrived with #2597 by mistake. It described local runs, not the product.
