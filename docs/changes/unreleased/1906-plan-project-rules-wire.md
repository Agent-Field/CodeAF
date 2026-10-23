---
kind: internal
title: Verify scoped project instructions through real plan workers
pr: 1906
surface: [engine]
invalidates: []
---

The production rule-forwarding fix already landed in #1627. Actual-terminal
coverage now verifies that both the plan worker and checker receive the applicable
project rule and exclude another project's rule. A live multi-step workflow also
checks the generated files and executes the recorded verification command.
