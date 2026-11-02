---
kind: fixed
title: Refuse invalid app Inbox answers before starting Undo
pr: 2015
surface: [engine]
invalidates:
  - "An Inbox answer returned an undo deadline before checking whether its conversation and question were reachable or its payload was valid. Preflight now shares the answer handlers' budget, required-option, landed-task verdict, Tell words, fuel amount and permission-scope checks and validates supervisor answers before the hold. Valid answers retain the five-second Undo hold; failures arising during that hold still have no separate failure event."
---
