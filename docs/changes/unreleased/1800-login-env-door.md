---
kind: fixed
title: login shell opt-out follows the shared environment lookup
pr: 1800
surface: [engine, docs]
invalidates:
  - "The login PATH opt-out read its owned variable directly through os and failed the environment law. It now uses the shared lookup, preserving empty-value opt-out and accepting the compatibility spelling."
---
