---
kind: changed
title: memory anticipates the current conversation and task without a remember command
pr: 1892
surface: [chat, engine, remote]
invalidates:
  - "Writes, replacements, edits, moves and restores were refused when any affected prompt exceeded 4,800 runes. Storage now accepts valid lines independently of prompt capacity; explicit person pins always ride in scope, and ordinary admission alone applies the prompt budget."
  - "Kept lines had one strength and overflow selection favored recency. Local activation now consumes practice history, project and topic context, and learned associations; the block reports omitted lines and the Memories tab explains the actual last-prompt selection."
  - "Automatic noticing had no explicit durability gate and use telemetry was unused. Ephemeral candidates are dropped, applied memories accrue replayable practice events, ignored lines decay, and daily local maintenance refreshes traces without model calls."
  - "Workers received the same memory section regardless of their assignments. Both worker paths now admit memory from their actual task briefs before execution, without a search request."
  - "The Memories tab exposed only a bounded initial snapshot. Load more memories now makes all active and archived rows reachable, including through hosted connections."
---

Ordinary conversation can teach a durable fact, revise it naturally, and have
the revision applied in a fresh conversation or task. Explicit remember commands
remain available as pins. Admission uses a bounded trace between exact daily
refreshes; conservative lexical deduplication does not merge arbitrary semantic
paraphrases. Automatic state-summary consolidation and optional embeddings are
not included.
