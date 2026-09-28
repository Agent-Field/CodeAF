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

Provider refreshes now reach each open picker through a lifetime-scoped, synchronized
subscription, including default-provider and failed listings. The add-provider list
keeps its selected row visible in short terminals and keeps the choice stable when
local discovery finishes. Custom addresses are checked off the UI loop before the
name/key steps; cancelled or superseded checks cannot reopen an old entry, and only
an authentication refusal asks for a key. Existing credentials survive edits when
a server exposes its model list publicly. Provider menus offer supported actions.
