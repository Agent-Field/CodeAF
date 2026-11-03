---
kind: fixed
title: Memory resume verification follows retained source citations
pr: 2284
surface: [engine]
invalidates: []
---

Pause and Resume checks now verify that the next finished turn publishes the
saved evidence it inherited. Later job timeline messages no longer make that
check fail after a successful pass. Brief requests are counted separately from
memory requests in the scripted provider. Runtime behavior and spending limits
are unchanged.
