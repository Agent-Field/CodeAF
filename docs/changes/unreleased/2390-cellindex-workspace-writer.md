---
kind: internal
title: Drain cellindex test writers before removing their workspace stores
pr: 2390
surface: [build]
invalidates: []
---
The four receipt-backed cellindex tests now allocate engine stores through the
shared EngineDir test helper. Each fixture first drains its recorder to join
pending group commits, then stops and joins spawned Furrow daemons before
TempDir removal. This prevents a background durability flush from restarting a
daemon and recreating refs.log during cleanup. Product daemon lifetime and Close behavior are unchanged.
