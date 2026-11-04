---
kind: fixed
title: Preserve measured spending during banking and across midnight
surface: [chat, engine]
invalidates:
  - Live interim readings previously disappeared before their billing callback returned.
  - Running calls previously kept all measured spending on their first observed day.
---

Keep interim usage visible until the legacy billing callback returns, then retire
it so settled usage counts once. The asynchronous ledger queue can still leave a
gap between callback return and ledger visibility.

Track the cumulative reading at each later local day boundary. Subsequent
observations count only the increase toward that day's cap, while all-days caps
retain the cumulative cost. Ended previous-day calls do not block a fresh day.
