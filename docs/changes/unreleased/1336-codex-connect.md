---
kind: added
title: a ChatGPT plan connects as the Codex model service, in the browser or with codeaf connect
pr: 1336
surface: [chat, engine, docs]
invalidates:
  - "Codex was not available as a connected model service. A ChatGPT plan now signs in from `/connect` or `codeaf connect codex` and its account models appear as `codex/<slug>`."
  - "A generic Codex model-list refresh could send the persisted `chatgpt` sentinel as a bearer. Every listing road now replaces it with the rotating account bearer and required account headers before reaching the backend."
  - "Every non-keyless model-service row opened key entry. The Codex row now says `browser` and uses the waiting card, copy affordance, and preferred-model move."
  - "The terminal command and chat panel owned separate model-connection outcome sentences. Both now read the same formatter in `internal/config`."
  - "The chat manual omitted `codeaf connect` and `codeaf disconnect`, and its terminal-verb gate temporarily exempted them. Both verbs are now documented and checked like every other terminal verb."
  - "A headless command refused to start with no OpenRouter key even when another service was connected. It now starts when any connected service holds its credential; a call to a service without one still fails when it is made."
  - "Connected-service documentation covered API-key services but not a ChatGPT plan's account list, limits, expiry, or unknown price. The Codex pages now state those boundaries and token-only accounting."
---

Codex uses the account's own visible model list and never exposes the persisted sentinel or rotating tokens to the catalog or screen. First run remains OpenRouter-only; device-code sign-in, OpenAI API-key sign-in, Codex base instructions, websockets, connector scopes, and the resident remain out of scope.
