---
kind: added
title: a workstream makes its own standing orders and previews their runs before they are switched on
pr: 2688
surface: [engine, factory, docs]
invalidates:
  - "Standing orders were made only by an agent's proposal card in a chat. orders.create now makes one for a workstream from the app (saved proposed, so nothing runs before its switch), and orders.preview answers when a when would run; both are announced by the hello word workstream_orders."
  - "An order said only fires_at, and only while active. It now also carries next_runs (at most three) whenever it repeats and may still run, switched on or not."
  - "A finished run's result folded your call and stopped into incomplete with nothing else to tell them apart. A run that is over now also carries outcome, its own word."
  - "A workstream task's came_from for a standing order was {kind, order_id, name}. It may now carry run, the order's run that made it; an answered event now carries option, the id of the answer chosen."
  - "The manual said a scoped order's results simply land in the task list. Each run is now one task (Came from the order, done by Afi, owned by the owner of incoming work, in the order's group), and a run that asks first asks on that task."
---

Workstreams v5 lane F-orders (live L2 finding 12: finance close, the weekly
client update and the newsletter were each built by hand because nothing made
an order inside a workstream). The desk holding the order makes each run its
task, in Personal and on an organization's server alike.
