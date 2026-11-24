---
kind: fixed
title: Client tool sources satisfy the generic-source law again
pr: 2766
surface: [engine]
invalidates:
  - "The instruction-size fix in PR #2729 added domain vocabulary to generic client-tool sources. Those sources now stay generic, and the domain rule-size test lives beside its rules."
---

The generic client-tool contract no longer contains app-domain vocabulary in its
source comments or validator fixtures. The rule-size regression lives beside the
rules it protects, with the same assertion. The generic-source law is unchanged,
as are the 8192-byte instruction allowance and detailed refusal messages.
