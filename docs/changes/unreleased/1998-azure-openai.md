---
kind: added
title: Azure OpenAI connects from AZURE_API_BASE and AZURE_API_KEY, and is spoken to in its own dialect
pr: 1998
surface: [chat, engine, docs]
invalidates:
  - "Every request to every service carried `Authorization: Bearer`, the router's `reasoning` object and `usage: {include: true}`, so an Azure OpenAI resource or gateway answered 401 or 400 to all of them and could not be used, not even as a custom endpoint. With `AZURE_API_BASE` and `AZURE_API_KEY` set, an `azure` service is connected at `$AZURE_API_BASE/openai/v1`; its key travels as `api-key`, and its requests carry `reasoning_effort` and `max_completion_tokens` and no `usage` opt-in."
  - "Every connected service came from a persisted `model_sources` row written by `/connect`. The `azure` service is the exception: it comes from the environment alone, has no `/connect` row, and is gone when either variable is unset."
  - "`max_completion_tokens` replaced `max_tokens` only on an openai.com, openai.azure.com or openrouter.ai address. It now also does so on any service whose `modelsource.Source` sets `OpenAIDialect`, whatever its host."
---

A gateway in front of Azure carries its owner's hostname, so the key header and
the dialect are data on the service (`KeyHeader`, `OpenAIDialect`) and never
read off the address. The crew reaches the service only by a pin, because its
models are deployment names no catalog carries.
