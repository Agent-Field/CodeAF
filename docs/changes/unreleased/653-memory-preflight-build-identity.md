---
kind: fixed
title: Bound memory preparation and detect stale engines after rebuilding
pr: 653
surface: [engine, chat]
invalidates:
  - "A newly built window could attach to an older background engine whenever their protocol versions matched. Attachment now compares process build identities too, replaces idle mismatches, and refuses busy mismatches without stopping their work."
  - "Memory selection blocked the main request behind the transport's two-minute header timeout and was priced as background work. Foreground recall now values interactive critical-path latency and shares the existing interactive silence cap across all its attempts, then fails open."
  - "Reflex calls bypassed the ordinary auxiliary adapter's deadline. All other reflex operations now share the existing reflex-tier patience across repairs and fallbacks."
  - "The empty-response indicator could blame the selected conversation model before it had been asked. Memory preparation now reports preparing saved context until lookup finishes."
---

A local September 8 incident showed a memory request waiting 120 seconds for
HTTP response headers before the first conversation-model request could start.
The attached engine also predated the rebuilt binary, despite sharing its protocol.
Tests cover same-protocol build mismatches, retaining busy hosts, process-stamp
precision, bounded repair deadlines, and the actual wait indicator's wording.
These fixes bound preparation; they do not guarantee main-model response latency.
