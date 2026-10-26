---
kind: changed
title: Clarify Memory controls and knowledge build progress
pr: 1973
surface: [chat, docs]
invalidates:
  - "Knowledge build progress showed imported chats and queued sources without stage percentages. It now distinguishes chat import from source processing and keeps page organization visible after sources finish."
  - "Knowledge build and search controls lacked pointer feedback. They now highlight under the pointer, while Knowledge and Saved memory switches underline on hover."
---

Memory uses the shared palette for headings, supporting text and numeric data.
Build progress wraps within the sidebar instead of clipping its totals when an
article is open. Source totals come from the same database reading as the
pending count, including sources captured before the importer saves its summary.

Percentages describe counted items in each stage, not overall completion or an
estimated finish time. The compiler keeps its ordered, single-writer batches;
its status remains readable while a provider request is outstanding. The change
does not claim a compilation speedup or increase model request concurrency.
