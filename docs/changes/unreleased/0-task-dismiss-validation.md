---
kind: changed
title: Task dismissal validates each producer under the same graph hold
pr: 0
surface: [engine]
invalidates:
  - "Task dismissal mixed node and run validation in one oversized decision path. Producer-specific checks now retain the same refusal text, visibility and atomic checkpoint hold; nothing a person sees has changed."
---

# Keep dismissal validation within one graph transaction

Task dismissal validates graph nodes and run records through separate producer
rules before checking live children and dependencies. Every check and the
checkpoint write still share the same graph lock; exact refusals, reversible
visibility, stopped work and unsupported external-plan behavior are unchanged.
