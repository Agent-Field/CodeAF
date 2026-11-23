---
kind: fixed
title: A fallback model is used for that run only and never becomes the conversation's saved model
pr: 2614
surface: [chat, engine, remote, app, docs]
invalidates:
  - "Was true: when a local launch replaced an unlisted configured model with the first listed model, the replacement became the conversation's saved model and the agent reopened on it. Is true now: the replacement serves requests only, the saved model stays the configured one, and the conversation returns to it as soon as it is listed."
---

The same holds after disconnecting the service of the running model. The replacement
stays in use while it is listed: later turns run on the model the disconnect sentence
names, and a refreshed catalog that lists another model first does not move the
conversation. An untouched chat
still saves the build's default when it follows a credit reading. Choosing a model yourself, including the replacement that is already
running, and connecting a service still save the choice. The desktop app reports the
configured model. A chat continued from another computer keeps its existing replacement rule.
A chat opened with `start_chat` and no model saves the requesting chat's configured model;
while that model is unlisted it runs on the requesting chat's replacement, and the
`started chat · <model>` receipt names the model it runs on.
