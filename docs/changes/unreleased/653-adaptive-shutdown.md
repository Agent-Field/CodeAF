---
kind: fixed
title: Wait for delegated workers when their session closes
pr: 653
surface: [chat, engine]
invalidates:
  - "Closing a session could cancel an adaptive run but leave its worker or naming callback writing after the records closed. Shutdown now joins accepted setup and callbacks before closing the session's stores."
  - "A completed run can still receive its pending name while the session remains open. Session shutdown cancels that request and includes it in the same bounded wait."
---

Adaptive runs share one additional shutdown round using the existing two-second
grace, outside engine locks. The scheduler still returns promptly on cancellation;
a noncooperative provider may outlive that grace. PERF.md records the bound.
