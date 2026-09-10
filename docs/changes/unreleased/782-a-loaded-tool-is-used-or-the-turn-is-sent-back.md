---
kind: fixed
title: a turn that loads a tool and stops without using it is sent back once
pr: 782
surface: [engine, docs]
invalidates:
  - "A turn could end right after `load_capability` with the loaded tool never called — the model answered the load with a plan (`Let me make the question.`) and no call, and nothing sent it back, so the question, picture or setting it loaded for never happened. checkpointReopen now reads that shape off the transcript (the turn's newest call was a load that answered `Loaded: …`, nothing called after it) and re-opens the turn ONCE with the loaded names, on typed turns as much as woken ones, spending no reader; a turn whose last words asked the person something is still left alone."
  - "internal/manual/chat/what-i-can-do.md listed three capability groups and never named `questions`; there are four, and `ask` is in the fourth."
---

Seen 2026-09-10 in the owner's own conversation on `z-ai/glm-5.3-flash`: "ask me a
question to give choices on what painting genres I like" loaded `ask`, produced
416 tokens of intention and no call, and the screen went idle with nothing asked.
The same sentence on `deepseek/deepseek-v4-flash` draws the question block, which
is how the wiring was proved right and the recovery proved missing. The person
sees `it loaded a tool and stopped before using it · asking it to go on` once, and
the model is told which tools it loaded and to call them in the same turn.
