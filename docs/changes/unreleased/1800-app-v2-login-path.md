---
kind: fixed
title: macOS app tool shells inherit the user's login PATH
pr: 1800
surface: [engine, docs]
invalidates:
  - "The desktop sidecar inherited launchd's reduced PATH, hiding tools configured in zsh login profiles. App startup now restores the login PATH synchronously before agents and factory work can start."
---

The non-interactive login probe runs once, with a three-second budget including
pipe cleanup. Failed probes retain inherited paths and existing common tool
directories. CODEAF_NO_LOGIN_ENV disables restoration; non-macOS launches are
unchanged. Tool commands still use bash -lc.
