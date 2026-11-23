---
kind: fixed
title: a red playwright suite names its failing checks to the gate instead of none
pr: 1978
surface: [engine]
invalidates:
  - "verify.FailingTests read nothing out of playwright's list, line or dot reporter output, so a red playwright suite named no failing checks. It now reads the closing `N failed` section, keeps the project in each name, leaves out checks listed as flaky, and drops the spec file's line and column so an edit above a check does not make it look new."
---
