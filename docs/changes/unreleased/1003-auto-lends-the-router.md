---
kind: changed
title: "`auto` lends the routing to OpenRouter and takes it back when its answers turn bad"
pr: 1003
surface: [chat, engine]
invalidates:
  - "`lane.talk: auto` was believed to run aforge's own lane chooser on every request, ranking the machines behind the model and demanding the admitted set on the wire. It does not: `auto` now sends the legacy preferences — the sort word, the strike ledger's order, the price ceiling — and lets OpenRouter route. aforge's chooser takes over only after a model's answers come back refused or unusable twice in a short while, and hands the choice back after about half an hour of good answers."
  - "The picker's `auto` row said it 'weighs speed against price each answer'. It says 'the router routes; aforge takes over if its answers turn bad'."
  - "`lane.talk: openrouter` was the only spelling of 'the router routes'. It is now `auto` without the takeover: the same router routing, and aforge never intervenes."
---

The chooser is not demoted, it is deferred. Every answer names the machine that
served it, so the belief ledger learns the router's picks exactly as it learned
its own; when the takeover comes the chooser ranks lanes it has been watching,
not strangers. Pins are untouched: a person's own word still outranks both.
