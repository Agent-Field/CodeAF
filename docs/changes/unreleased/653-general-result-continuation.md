---
kind: fixed
title: Long checker results keep an ordinary read path to their later lines
pr: 653
surface: [engine, chat, docs]
invalidates:
  - "The checker's 8000-byte result cap discarded read's continuation footer, so asking again could return the same first fragment. The cap remains; read now keeps its ordinary line-offset footer inside it, and other long results name the canonical saved-output file that read can page."
---

The continuation uses the existing content-addressed droppings beside the
commissioning conversation. It works across prose, data, code and command output
without writing into the work being judged or spending another model call.
