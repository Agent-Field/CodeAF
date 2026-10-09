---
kind: fixed
title: senior-dev reads its window from codeaf offline, refuses tiny models, keeps progress on compaction
pr: 1791
surface: [engine, chat, docs]
invalidates:
  - "senior-dev sized every model at 16,384 tokens when models.dev could not be reached. It now reads the window from the model catalog codeaf keeps for the profile, and only guesses 16,384 when neither knows the model, saying so on its page and in its log."
  - "senior-dev started on any model it could size. A model the person asked for that is known to hold 32,768 tokens or fewer is now refused before its first call with `senior-dev cannot work with <model> (<n> tokens): …`, and nothing is spent; a crew seat that small is left out with a note instead."
  - "When a compacted history did not fit, senior-dev replaced the summary with a stub that kept no progress. The stub now carries as much of the newest summary as fits, and the changed-files list."
  - "The summary length budget assumed 2,048 tokens of fixed context. It now measures the system prompt, tools and pins, and never asks for more than the summarizer's output limit."
  - "A context-overflow pin applied to one senior-dev session. It now applies to every session on the same model for the rest of the run."
---

A CyberGym run in a container that could not reach models.dev sized a 1M-token model at
16,384 tokens. It compacted 115 times in 25 minutes, and 113 of those compactions erased
its progress. The do stage delivered nothing. codeaf's own catalog in that container
already listed the real window.

A senior-dev shell run now waits up to one catalog fetch (`catalog.FetchTimeout`) for the
profile's model list before it starts, so a fresh profile writes the list senior-dev
reads.
