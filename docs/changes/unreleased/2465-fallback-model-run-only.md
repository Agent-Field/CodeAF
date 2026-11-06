---
kind: fixed
title: a fallback model is used for that run only and never becomes the conversation's saved model
pr: 2465
surface: [chat, engine, remote, app, docs]
invalidates:
  - "Was true: when a local launch replaced an unlisted configured model with the first listed model, the replacement became the conversation's saved model and the agent reopened on it. Is true now: the replacement serves requests only, the saved model stays the configured one, and the conversation returns to it as soon as it is listed."
---

The same holds after disconnecting the service of the running model and for the implicit
credit-default follow. Choosing a model yourself, including the replacement that is already
running, and connecting a service still save the choice. The desktop app reports the
configured model. A chat continued from another computer keeps its existing replacement rule.
