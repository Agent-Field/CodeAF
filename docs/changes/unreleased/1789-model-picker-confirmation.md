---
kind: changed
title: Enter confirms a model and closes its picker immediately
pr: 1789
surface: [chat, docs]
invalidates:
  - "Enter used to apply a model and leave the picker open until Esc. It now selects the model and closes the list immediately, returning to the conversation, task room, settings, Home draft or task composer."
---

The draft is preserved when the model is chosen. A list with no matching model
stays open; Enter inside provider controls keeps their existing navigation.
