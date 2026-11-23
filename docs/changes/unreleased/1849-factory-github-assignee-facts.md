---
kind: fixed
title: Factory intake keeps every GitHub assignee without choosing one from a team
pr: 1849
surface: [engine]
invalidates: []
---

An absent or malformed assignee list stays unknown, while an empty list means nobody is assigned. A new job receives an execution owner only from a sole valid assignee. Later GitHub reads update the source fact without replacing execution ownership.
