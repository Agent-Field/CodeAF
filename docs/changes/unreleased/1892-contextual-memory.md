---
kind: changed
title: contextual memory protects durable rules from progress updates
pr: 1892
surface: [chat, engine, remote]
invalidates:
  - "Writes, replacements, edits, moves and restores were refused when any affected prompt exceeded 4,800 runes. Storage now accepts valid lines independently of prompt capacity; explicit person pins always ride in scope, and ordinary admission alone applies the prompt budget."
  - "Kept lines had one strength and overflow selection favored recency. Local activation now uses proof-of-use history, project and topic context, and learned associations. Durable rules have a protected budget share, specific context matches take precedence over generic repetition, and the Memories tab explains the actual prompt selection and composition."
  - "Automatic noticing had no explicit durability gate and use telemetry was unused. Ephemeral candidates are dropped, applied/confirmed/merged memories reinforce practice, retrieval and surfacing remain neutral, contradictions reduce strength, and daily local maintenance refreshes traces without model calls. Derived labels correct legacy progress paragraphs without rewriting their words or original kind."
  - "Workers received the same memory section regardless of their assignments. Both worker paths now admit memory from their actual task briefs before execution, without a search request."
  - "The Memories tab exposed only a bounded initial snapshot. Load more memories now makes all active and archived rows reachable, including through hosted connections."
---

Ordinary conversation can teach a durable fact, revise it naturally, and have
the revision applied in a fresh conversation or task. Explicit remember commands
remain available as pins. Admission keeps explicit pins unbounded, protects at least 40% of the
remaining prompt budget for durable rules when needed, and holds admitted lines
while conversation context stays unchanged. Per-assembly aggregate telemetry
records rune shares by derived kind, with pin consumption separate; no memory
content appears in those logs. Admission uses a bounded trace between exact
daily refreshes; conservative lexical deduplication does not merge arbitrary semantic
paraphrases. Automatic state-summary consolidation and optional embeddings are
not included.
