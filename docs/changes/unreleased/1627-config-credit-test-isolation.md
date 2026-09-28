---
kind: fixed
title: credit fixtures keep developer credentials out of profile balance tests
pr: 1627
surface: [chat]
invalidates:
  - "The config credit tests could fail whenever a developer exported a provider key, because it outranked their fixture keys. They now isolate both provider-key variables and separately verify environment-key precedence and stale-balance invalidation."
---

Addresses the internal/config instance of #1489. Product key resolution and
balance behavior are unchanged; the combined acceptance gate can exercise
these fixtures even when the launching shell carries provider credentials.
