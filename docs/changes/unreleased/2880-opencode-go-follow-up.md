---
kind: fixed
title: OpenCode Go routes only the models it lists and checks the key's plan
pr: 2880
surface: [chat, engine, docs]
invalidates:
  - "Was true: the crew treated OpenCode Go as serving every catalog model of the vendors it maps, and connecting accepted any key the listing accepted. Is true now: the crew offers only the exact model IDs OpenCode Go lists (the preferred model until a listing is cached), and connecting asks OpenCode's usage meter whether a Go subscription covers the key and refuses one it does not."
  - "Was true: the OpenCode Go change entry said the subscription has no metered overflow. Is true now: OpenCode's Console can spend credits through the same endpoint after plan limits (Extra Usage), and the manual says so."
---

The wrong-key hint names OpenCode Go's `oc_sk_` prefix, and keys of that shape are
redacted from errors and logs even when they were never registered. The first-run
provider chooser keeps the selected row visible on a short terminal instead of
dropping OpenRouter once the list is longer than the screen.
