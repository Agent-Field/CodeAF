---
kind: internal
title: Eviction fixtures drain engine writes before removing temporary storage
pr: 2301
surface: [engine]
invalidates: []
---

The cell budget fixture now flushes pending group commits and waits for its
spawned engine daemon to exit before TempDir cleanup. Previously it removed
storage while the daemon could still write workspace refs and group-commit
metadata, intermittently failing the least-recently-opened eviction test with
"directory not empty". The fixture had not called a close boundary; product
close behavior is unchanged.
