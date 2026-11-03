---
kind: added
title: codeaf keeps conditions on steps added to one job
pr: 0
surface: [engine]
invalidates:
  - "Added job steps previously refused when through the app and HTTP edit route; direct edits accepted unknown conversation and check conditions and silently dropped unknown approval conditions. Added conversation, check and approval steps now keep a supported condition, and unknown conditions are refused."
---

The engine announces `factory.job_step_edit_when`. An added step may carry
`when: touches auth`; jobs outside auth, billing and crypto skip it. The other
supported conditions are `always`, `thin`, `large` and `has ui`. Omitting `when`
keeps the existing behavior. Existing steps cannot gain a condition through this
edit, and fanout remains unsupported.
