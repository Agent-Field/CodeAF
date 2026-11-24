---
kind: added
title: an organization's standing orders are kept and run by its server
pr: 2738
surface: [engine, chat, docs]
invalidates:
  - "After #2731 an organization's workstream had no standing orders at all: the clock passed them over and orders.create refused them. The organization's server now keeps them (/v1/workstreams/{id}/orders, factory.org_workstream_order_* on the wire), everyone in the workstream sees them, and only the person who looks after it adds, switches or removes one."
  - "An order's run in an organization was a general agent with bash, read and services on one person's computer. It is now one task on the server, done by the workstream's agent in the task's own seat with only workstream_pages and the ws_* tools; it asks its owner Go / Skip this one / Not right unless Start without asking covers its budget, and Stop on the task ends it."
  - "A server that cannot run Afi (no runner or no organization model key) refuses to make or switch on an order with 409, and lists runs:false."
  - "The manual said an organization workstream's chats stay on the person's computer. An organization's workstream now starts no chat at all (codeaf-desk #556), and the manual says so."
---

Fix lane X1, E4 F0 part 2 and E4 F6. The desk reads and writes these orders
through the org forward when the server announces org_workstream_orders.
