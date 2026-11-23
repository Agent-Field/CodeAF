---
kind: fixed
title: A routing rule's priority replaces the one Jira or an inbox entry sent
pr: 2482
surface: [factory, engine]
invalidates:
  - "A routing rule's then.priority applied only to a job with no priority at all, so a Jira ticket at Medium routed by a p1 rule stayed p2. Now a priority the source sent (set_by source) is replaced by the rule's; a person's own priority mark still wins over a rule."
  - "Nothing drove the general factory (routing rules, the AI filter, the inbox, the Jira connector) end to end against a real engine. Now internal/e2e/factory_general_e2e_test.go does: opt-in with FLE2E=1, real bin/codeaf app over stdio, real model, real factory-demo, jirastub in process."
---
