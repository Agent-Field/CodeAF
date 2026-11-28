---
kind: changed
title: A standing order has one run waiting for your go, and can be told to stop asking
pr: 3005
surface: [app, engine, chat, docs]
invalidates:
  - "A run of an order that asks first could, once it got past the clock's own check, wait beside another run of the same order, so the same work could be asked about twice and a go to each would run it twice. A run that would wait beside one already waiting now ends at once, as skipped, with the line \"folded into the run already waiting for you\"; the run that was waiting keeps its id and the time it began to wait, and a go runs the work once, folding any other run of the order still waiting into the one answered."
  - "The only ways to trust an order were five go's, or setting its trust from its settings with the run waiting on it left waiting. Setting an order's trust to trusted now starts the run waiting on it in the same write, without counting a go, so no trusted order is left asking; a paused order keeps its waiting run waiting. A new answer, always, shown as \"Go, and stop asking\", runs the waiting run, counts as one go and trusts the order, so its next times run without asking."
---

A new order still asks first, five go's still make it trusted, and skip, not right, run now, reminders and watches that only look are as they were. Agreeing to an order with its trust set to trusted already saved it trusted, and its first run already started with no question; that is now pinned by a test. An engine that takes the new answer names `orders.always` in hello, so a surface draws the button only against one that does; `orders.run` refuses `always` as `bad_params` against one that does not. The desktop half, which draws the button and the trust control, comes separately.
