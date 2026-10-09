---
kind: changed
title: Place Home names the model its first message will really run on
pr: 9999
surface: [engine]
invalidates:
  - "The desktop Home composer always showed the saved Conversation role's model. A place's own default model (or one inherited from a parent) overrides that role on the new chat's first turn, so the chip named a model that did not run. It now asks GET /places/{id}/effective-model, which resolves the model field through the same placegraph policy rule the engine applies, and shows the place's model, with the place named beneath it."
  - "When a place decides the model, changing the Conversation role cannot change what runs, so the chip offers no swap there. When places disagree and nothing above them decides, nothing is applied: the chip shows the role's model and says the choice is made in the chat. When the engine cannot say, the chip names no model rather than a guess."
---

The route is read-only and authenticated, opens no session, writes nothing and
calls no model. PR number 9999 is a placeholder until one is assigned.
