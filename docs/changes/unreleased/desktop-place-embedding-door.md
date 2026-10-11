---
kind: added
title: Places embeddings use the engine account and a separate billing role
surface: [engine, desktop, docs]
invalidates:
  - "The places door could only ask filing and suggestion questions, and had no embeddings transport. It now accepts the six organiser roles at the low tier and carries 256-dimensional embedding batches through the engine, billed as placeembed."
---

The bridge supplies the primary-space Embedder adapter. The organiser pipeline
is wired by its own integration lane; this change does not apply place plans.
