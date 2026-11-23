---
kind: added
title: codeaf app serves standing orders, which run nothing before ratify and earn trust
pr: 0
surface: [engine]
---

Was true: `codeaf app` served no `orders.*` methods; an automation was saved
only on the card's yes, and every saved automation ran alone on its schedule;
the app held no window, so no clock ran for it.

Is true now: under the app's launch (`standingOrders`) the `automation` tool
saves a proposal as `proposed` before its card is up, and nothing runs before
ratify (the card's Save or `orders.ratify`). Work proposed this way asks before
each run (a new `asking` run phase, answered go, skip or not right) until five
go's make it trusted; one not right resets it. `stopped` is a new status that
is never resumed, and an automation now carries `answersIn`, `crew` and
`wont`. The app holds an open window for its whole life and serves
`orders.list/runs/ratify/change/not_now/pause/resume/stop/run` with the events
`order.changed`, `order.removed`, `order.asking`, `order.fired` and
`order.missed`. The terminal saves on the yes, as before.
