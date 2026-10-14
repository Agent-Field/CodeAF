---
kind: fixed
title: review resolves model words and linked roots and formats final answers
pr: 1818
surface: [chat, engine, docs]
invalidates:
  - "The shell resolved the main model flag but passed an explicit light model word unchanged. Review and security now resolve both through the same profile catalog, refusing unknown words before calls start."
  - "Read tools compared resolved file paths against an unresolved repository root and refused ordinary files under a linked root. Both spellings of the owned root now work while external paths and links remain refused."
  - "Final review answers requested JSON only through prompt instructions. They now request native schema formatting when the cached catalog publishes support, with one prompt-only fallback for an explicit unsupported-format refusal."
  - "The macOS installer surface regression compared a physical output path with a linked temporary-path spelling. It now checks the exact resolved installation target while preserving installed-byte and invocation-count checks."
---

Every review answer is still checked against the original full schema. Invalid schemas, account or ceiling refusals, cancellation and provider outages do not take the formatting fallback. Model defaults and turn, time and cost ceilings are unchanged.
