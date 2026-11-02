---
kind: internal
title: Client identity refusal checks identify the refused message
pr: 0
surface: [app]
invalidates: []
---

The restart check now distinguishes a refused message from background memory
work for an earlier accepted exchange. It still rejects any model request
containing the refused message and verifies that only accepted messages reopen.
