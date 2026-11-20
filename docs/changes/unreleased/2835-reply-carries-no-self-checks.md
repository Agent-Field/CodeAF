---
kind: changed
title: a finished reply is told to carry no self-checks or second drafts
pr: 2835
surface: [engine, chat]
invalidates:
  - "Was believed: the leaked \"Lounge: … Links: default. Done.Fixed.\" in qloop run10's flyer reply was the engine merging a model's reasoning channel into its answer. Is true: OpenRouter counted 4 reasoning tokens out of 142 for that call, the 4 the transcript kept as reasoning, so the model wrote the self-check on the content channel with no fence. The engine's split was right and is now pinned for reasoning interleaved between two runs of content. The system prompt's answer section now says final answers \"carry no self-checks or second drafts\"."
---

No text is filtered out of a reply. The provider marked nothing, so the engine
has nothing it can strip honestly. The prompt clause is the mitigation. It was
seen in 1 of 555 final replies across the surviving eval transcripts.
