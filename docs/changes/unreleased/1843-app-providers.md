---
kind: fixed
title: codeaf app offers every provider the terminal connects, and connects each the way the terminal does
pr: 1843
surface: [engine, docs]
invalidates:
  - "providers.list named only the providers a pasted key connects, and setup could offer no more. It now names every provider the terminal's first-run screen offers, in its order — OpenRouter, Ollama, Codex, DeepSeek, Z.ai, Moonshot, MiniMax, Alibaba Qwen, ai&, a custom endpoint — and each says what connecting it asks for (kind key, browser or address; regions; key_optional; address)."
  - "keys.check was one listing request for every provider, so a provider with no listing (Moonshot, MiniMax) read as a key that did not work and a Z.ai, Moonshot or Qwen key was always billed pay-as-you-go. Every provider but OpenRouter is now checked by the terminal's own requests, region and plan included, and answers a connection that keys.set keeps in the profile (never the key). OpenRouter's check is still exactly one request."
  - "keys.check and keys.set always needed a key. A key_optional provider (Ollama, a custom endpoint) takes an empty one and is then connected with none; its address is kept in the profile."
  - "The ChatGPT (Codex) sign-in could not be started from the app, and config refused it in the app's process. providers.sign_in now starts it; its rotating tokens stay in the profile exactly as the terminal keeps them, the one credential the app does not hold, and keys.remove signs it out."
  - "A model a keys.check answered for a direct provider was a bare id, which routed to OpenRouter when chosen. It is now spelled as models.list spells it (deepseek/deepseek-v4-pro)."
  - "models.list offered nothing for a provider that lists no models. It now offers the model the terminal moves a conversation onto when that provider connects, and a provider connected through keys.set has its models fetched in the background."
  - "An Ollama row could only ever be at localhost:11434. A row may now carry an address of its own (modelsource.Source.AddressOwned); the terminal never writes one, so its Ollama connection is unchanged."
  - "The manual said the app's browser sign-in was OpenRouter's only and that the ChatGPT sign-in stayed in the terminal. Its new section, Connecting a provider from the app, says what the app asks each provider for and how it checks them."
  - "codeaf app remembered a process half that would not open for want of a key for the whole launch, so on a first launch nothing connected in setup reached models.list or a new agent until the app was opened again. An open refused for want of a key is now tried again by the next caller, and models.list waits a moment for a just-connected provider's list."
  - "In the app's process, a profile whose service rows the terminal had written with keys refused every further service write. A row whose key is byte-identical to the one already on disk now passes; a key the profile does not already hold is still refused."
---

config.ConnectService is now config.ProveService (the requests, no write) and
one write, so the terminal and the app prove a key with the same code; the
terminal's behaviour is unchanged. The protocol changes are additive:
Provider gains kind, key_optional, address, regions, region and plan; keys.check
and keys.set gain region, address and connection; KeyCheck gains connection.
