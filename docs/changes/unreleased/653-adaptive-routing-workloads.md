---
kind: fixed
title: Learn generation work and keep successful prompt caches stable
pr: 653
surface: [engine, chat]
invalidates:
  - "Every interactive request was scored as 400 readable tokens and background work as 2000 hidden tokens. Completed receipts now teach model, tool-availability and reasoning-setting workloads; conversation history supplies the fallback, and unknown output stays unknown."
  - "A successful long-prompt answer reporting zero cached tokens released its endpoint preference. It now keeps that preference because a changed or expired prefix can warm the next request. Failures and the existing price ceiling still release it."
  - "The streamed wait monitor could time the chooser's sampled first endpoint while cache affinity sent another endpoint first. Its generated choice now includes the actual wire preference."
  - "A text batch counted as one visible token and tool-only streams taught no generation interval. Stream progress now measures accumulated text size, and tool fragments start the generation clock."
  - "A watched transport fault could retry the same encoded request three times before its existing recovery controller tried an affordable alternative. It now hands the fault to that controller immediately when an alternative is available; rate limits retain their backoff."
  - "Only successful response bodies had an idle watchdog. Retryable error bodies now receive the same bound. The documented x-session-id header accompanies the existing cache lineage, with stable hashing for identities beyond its protocol limit."
  - "Cache affinity could override a borrowable explicit pin, and failure recovery could leave a strict primary without consent. Both retain the person's routing constraint. Recovery also uses a request's effective model rather than the client's default model."
---

Answer-size evidence decays using the existing routing half-life, is capped by
the request's output limit, and persists through the existing shared journal
with bounded retention. Both transport paths learn from usable, completed
answers; capped, filtered and incomplete replies are excluded. The existing
adaptive stall controller and rescue spending limits remain in force.

An unknown output size no longer removes otherwise eligible alternatives through
an assumed zero output bill or an unjustified output-price comparison. Known
workloads still use the existing frontier and cost arithmetic.

Regression tests cover shared persistence and compaction, aging, eviction,
both response transports, throughput-sensitive tool work, output caps,
incomplete receipts, cache preference, and agreement with the wait monitor.
