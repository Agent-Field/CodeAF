---
kind: fixed
title: New team workers start their first turn after delayed membership
pr: 1946
surface: [chat, engine, docs]
invalidates:
  - "A worker joining more than twenty seconds after its conversation opened could stay idle until a second message; its own start now wakes it through the ongoing traffic watch."
---

The worker reads its manager's initial brief once, even when conversation setup
delays membership beyond the short startup watch. Existing auto-wake settings,
start identity checks, delivery cursors, and execution limits still apply.
