---
kind: changed
title: unify provider vocabulary, background-list connected provider models, and ease adding providers
pr: 1513
surface:
  - chat
  - docs
invalidates:
  - "the entity serving models was called service or connection; it is now called provider everywhere."
  - "the openrouter routing target was called provider; it is now called host."
  - "external tools were called connections; they are now accounts."
  - "/model only listed models from OpenRouter on launch; now every connected provider lists models in the background."
---

Unifies model-serving vocabulary across UI, settings, manual, and CLI on 'provider',
uses 'host' for OpenRouter routing destinations, warms cold provider caches at launch
off the event loop, enables ctrl+r multi-provider refresh, and adds loopback port
probing for local model servers.
