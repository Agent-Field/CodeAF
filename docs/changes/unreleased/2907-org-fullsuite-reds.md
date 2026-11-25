---
kind: internal
title: hosted-chat environment variables are registered as operator plumbing
pr: 2907
surface: [factory, build]
invalidates:
  - "The org server's and sessionhost's CODEAF_* variables were absent from config.OperatorEnvPins, so the full suite's environment registry test failed on org/main; they are now listed there as read-only plumbing."
---
