---
kind: changed
title: a workstream's Afi no longer proposes a decision for someone to keep
pr: 2723
surface: [engine, factory, docs]
invalidates:
  - "Was true: the organization server's Afi could suggest a decision with workstream_pages ask decide, and it waited in Decisions until a person kept it. Is true now: workstream_pages offers no decide; decisions record themselves when an Ask is answered, a request changes shared work or a rule is set, and no screen has a Keep. Decisions shows only those, so a proposed one was never visible or keepable."
  - "Was true: the chat manual said nothing about a Personal workstream task left Running after an engine restart, or a Start pressed while the chat was busy. Is true now: an engine start makes such a task Interrupted with the reason on its thread, and a busy chat's Start is queued with a Queued note (desk #536, #537)."
---
