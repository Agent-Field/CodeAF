---
kind: changed
title: '`simple` is the routing row this build ships with, and it is the whole algorithm'
pr: 1019
surface: [engine, chat, docs]
invalidates:
  - "`latency` was the default, and an unwritten `routing` row meant the request asked the router to sort by speed under a price ceiling. The default is `simple`: with no lane pinned the request carries NO provider object at all and the router's own default routing answers it, and with a lane pinned that pin is the whole request (`only`, fallbacks off). `latency` and `price` are unchanged and one word away on the same row."
  - "The row's cycle read `latency`, `price`, `simple`, `off`. It reads `simple`, `latency`, `price`, `off` — the default first, as every cycle row in the sheet is ordered. `config.RoutingModes`, the settings hint, the Providers-tab list in the manual and the `/settings` panel's own sentence all moved with it."
  - "A call with nobody's row written was routed BY WHO WAS WAITING: a person's own turn asked the router for the fastest endpoint and an errand nobody was watching asked for the cheapest, decided per request inside the adapter. That split is gone. Every call falls to one answer, `provider.DefaultRouting`, and the routing intent — still stamped by a dozen call sites — is now read only for what a wait is worth to the lane chooser and for how much of an answer anybody is reading. Comments in `internal/session` that said an errand `routes by price rather than by speed` said something this build no longer does."
  - "The one-token measurement bought while you type, and the endpoint the adapter asks for again to keep a prompt cache warm, ran on a home nobody had configured. Neither does now: both are `latency` and `price` machinery, and under the shipped row the request carries no preference of ours to carry them. The lane sheet the router publishes is still fetched — it is free, and it is what fills the machines a person picks a pin from — so `/model`'s fold still opens with numbers on it."
  - "An unreadable or unknown `routing` word fell back to `latency`, in `config.RoutingAt` and again in `provider.ParseRoutingStrategy`. Both now fall back to the shipped row, and the vocabulary check itself lives in one place (`config.routingWritten`, with `config.RoutingWord` for a row already in hand). `internal/tui3`'s `auto` row asks that door too, so an empty or unknown word draws the `openrouter's own routing; aforge stays out` sentence rather than promising a takeover that does not run."
---

The owner's ruling is that what the picker shows, what is selected and what the call
log records must be the same thing that went on the wire, and the provider-selection
algorithm was the one decision in a turn nobody could watch being made. So the
algorithm becomes opt in rather than opt out. Nothing is deleted: `latency` still
sorts by speed under its price ceiling, times every answer and demotes a machine that
keeps being slow, and `price` still ranks on price alone — a person who wants either
writes it on the `routing` row and gets all of it back, probes and rescue and warm
cache included.
