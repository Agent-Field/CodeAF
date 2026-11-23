---
kind: fixed
title: Recover saved factory PR work and track unresolved review threads by identity
pr: 1899
surface: [engine, chat]
invalidates:
  - "An invalid Cargo invocation could remain blocking after corrected checks covered its complete scope."
  - "Outdated inline comment positions were treated as resolved and could manufacture new paid repair rounds."
  - "Interrupted Follow PR work repeated passing checks and productive retries consumed the reconnect waiting window."
---

- Recover corrected Cargo verification without clearing unrelated failures; retain and reuse current-checkout check receipts on Follow PR retries.
- Track review comments by stable identity and explicit resolution, including outdated inline threads, and acknowledge only completed follow-up.
- Keep GitHub monitoring visible during recoverable pauses; restore eligible saved Auto jobs through a bounded recheck without repeating publication.
- Exclude productive running time from reconnect waits and show external contributor agreements separately from running CI.
