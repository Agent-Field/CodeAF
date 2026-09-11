---
kind: fixed
title: Keep the deadline pacing regression valid under concurrent test load
pr: 653
surface: [engine]
invalidates:
  - "The pacing fixture could spend its entire landing reserve on scripted work delays and report a deadline error unrelated to the pacing assertion. It now crosses the pace point, lets the next work call reach its deadline, then answers the landing immediately."
---

The regression still requires one clock notice before the deadline landing. The
real ten-second wall and all production limits are unchanged. Five consecutive
focused runs pass; the full repository gate covers related execution behavior.
