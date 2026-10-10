---
kind: added
title: Plan card executor and Go/Edit/Cancel routes
surface: [desktop, chat]
invalidates:
  - "The three /sessions/{id}/plan/{plan}/{go,edit,cancel} routes answered 501. They now run, edit and cancel plans held by the conversation's PlanBook; the model-facing plan tool exists but is only on the belt when Config.PlanCards is set."
---

A plan that reaches beyond the chat waits for Go; one inside it runs at once.
Results group into one receipt, a deleted target is a skipped line, and Cancel
touches nothing.
