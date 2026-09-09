---
kind: fixed
title: streamed reasoning is replayed as completed blocks
pr: 746
surface: [engine, chat, docs]
invalidates:
  - "Streamed reasoning details were appended as separate token fragments. Compatible text and summary fragments now form one completed block before the next tool-loop request."
  - "Preserving reasoning did not mean its streaming representation was ready for replay. Block identities, signatures and opaque data remain intact while text fragments are assembled once at the completed assistant boundary."
---

This corrects continuation history without another model call, a model-specific
rule, a reasoning-effort override or a generation limit. The captured CPF trace
demonstrates fragmentation; improved benchmark quality remains to be measured.
