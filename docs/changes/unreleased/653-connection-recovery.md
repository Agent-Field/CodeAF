---
kind: fixed
title: Wait for endpoint reachability after a lost connection
pr: 653
surface: [chat]
invalidates:
  - "DNS failures exhausted three generation retries and could fan out across providers behind the same unreachable router. Pre-send connection failures now share a bounded, cancellable reachability wait before resending."
  - "A connection outage looked like a slow model or an ordinary retry. Its phase now says waiting for connection, with elapsed time and no invented recovery deadline."
  - "Authenticated media requests failed immediately on a pre-send DNS/dial error. They now use the same recovery primitive, while an ambiguous lost response to an accepted job is not resubmitted."
---

Healthy requests make no probe. During recovery, one credential-free HEAD check
per adapter reaches only the configured origin, follows no redirects, and sends
no inference body. The wait is capped at two minutes and respects shorter caller
deadlines; canceling one waiter does not stop its siblings, and canceling the
last waiter stops the probe. Existing response, pacing and stream recovery
policies remain distinct. A spent connection wait does not start another model
or provider recovery ladder. Optional paid lane measurements pause during it.

Routing deadlines pause while the connection is down and restart when reachable.
Recovered calls do not teach provider latency or stream duration, so a local
outage cannot demote a serving endpoint or inflate its learned wall.
