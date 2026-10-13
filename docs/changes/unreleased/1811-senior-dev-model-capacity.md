---
kind: fixed
title: senior-dev checks selected-service model capacity before starting
pr: 1811
surface: [engine, docs]
invalidates:
  - "A shell run warmed only the default service catalog, so a model from another connected service could fail sizing despite that service publishing its window. It now warms the catalogs for the selected high, low and frontier models."
  - "A missing or zero window could reach senior-dev's pipeline and fail during work. All remaining pools now refuse before stages or model calls, and provider-supplied project limits remain supported."
---

