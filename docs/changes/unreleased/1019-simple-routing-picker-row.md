---
kind: fixed
title: the model picker's `auto` row stops promising a takeover the `simple` routing never does
pr: 1019
surface: [chat, docs]
invalidates:
  - "The `auto` row in the model picker's lane fold read `router routes; aforge takes over if answers turn bad` under every routing row. Under `routing: simple` it reads `openrouter's own routing; aforge stays out`, because that mode disconnects the chooser, the gate, the hedge and the probes — nothing on aforge's side takes anything over."
  - "That row also named the machine the next turn would land on (`— cloudflare now`), and the settings panel's `your model` row carried the same prediction as `auto (cloudflare now)`. Under `simple` neither is drawn: an unpinned request carries no provider object at all, so the machine that answers is OpenRouter's choice and no prediction here can be honest. The `no rescue` chip is gone there too — there is no rescue under any setting of the speed guard."
  - "The settings sheet's `lane` row explained itself with a sentence written beside the row (`auto picks the fastest one each answer`). It has no sentence of its own any more: the panel and the picker both read one door, so the two cannot say different things about one routing row."
---

The surface kept a single boolean reading of a row that now has four answers — `routingOff`
— which is why one sentence could be false under the fourth. It keeps the row itself
instead, and everything that says what `auto` DOES asks the one function that turns that
row into a promise.
