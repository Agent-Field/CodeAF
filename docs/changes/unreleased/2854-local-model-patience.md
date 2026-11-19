---
kind: fixed
title: A slow local model gets ten times the wait, and response.patience changes it
pr: 2854
surface: [engine, chat, docs]
invalidates:
  - "A model on your own machine or network (Ollama, LM Studio, llama.cpp, a box on the LAN or Tailscale) was held to the hosted figures: 2 minutes for response headers, 10 seconds of silence after headers, 45 seconds quiet mid-reply, a 5-minute reply wall, 150 seconds for a non-streamed reply, and a 2-minute compaction summary. Under the default `response.patience: auto` a local endpoint now gets ten times each of those, and the turn's give-up stretches with them; hosted endpoints keep the old figures."
  - "A slow request to a local endpoint was sent again to the same server after 10 seconds, so one GPU did the work twice. Under `auto` a local endpoint is never sent a duplicate unless the gateway lists more than one provider for the model."
  - "Nothing could make codeaf wait longer for a quiet model; `response.attempts` only scaled the give-up. `response.patience` (env `CODEAF_RESPONSE_PATIENCE`, the `patience` row on the Models tab) takes `auto` or a number from 1 to 100 that applies to every endpoint the same way, and `1` is exactly the behaviour before this change. A bad value is refused naming the row or the variable."
---

A local model can spend minutes reading a long prompt before its first token,
and it is the only machine there is to ask. "Local" is decided from the address
alone: loopback, private, carrier-grade and link-local addresses, `.local`,
`.lan` and `.internal` names, `host.docker.internal`, a single-word host, or the
Ollama connection wherever it runs. A saved row reaches an open conversation on
its next model request.
