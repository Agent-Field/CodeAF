---
kind: fixed
title: Desktop places transport carries typed routes and generation guards
surface: [desktop, engine, docs]
invalidates:
  - "The desktop places client kept its wire types in client.ts and omitted session and rail operations. The wire contracts now live in wire.ts, and the client exposes guarded place, rail, context, source and session operations."
  - "Place Undo, context choices and starting a place chat rejected an ifGeneration field. Their decoders now accept the guard and preserve the existing stale-generation refusal sentence."
---

The pure places-record reducer ignores older generations and identical replay,
while accepting soft rail changes within one generation. Existing imports from
client.ts still work. A caller may supply the generation it rendered; otherwise
the transport reads the graph before writing. No automatic conflict retry is made.
