---
kind: fixed
title: A machine without bash runs a hyphenated check command instead of failing to parse it
pr: 2152
surface: [engine]
invalidates:
  - "Was true: on a machine whose only shell is sh (dash), every command wrapped for check observation failed with a syntax error before it ran. Is true now: it runs unobserved with its own exit status, as the sh fallback intends."
---
