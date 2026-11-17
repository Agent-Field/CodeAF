---
kind: internal
title: Async regression tests wait for completed work
pr: 2787
surface: [app, engine]
invalidates:
  - "Was true: the team loop test assumed stream closure meant the member's finished event had reached traffic. Is true now: it joins the deferred event writer before advancing its manual clock or starting another round."
  - "Was true: daily-limit tests ended their worker after a fixed interval and the reopened-drive test checked a cancelled sleeper immediately. Is true now: they wait for the persisted daily pause or cancellation acknowledgement before asserting."
  - "Was true: mark, answered-ask, plan-files and ordinary knowledge retry tests inferred readiness from elapsed time or a drained worker group. Is true now: they wait for the step starting, published task or job state, and completed worker passes."
---

No product behavior changes. The daily-limit fixture also joins its worker
before restoring the transport or removing its throwaway home. All original
money, retry, loop-breaker and drive-retirement assertions remain.
