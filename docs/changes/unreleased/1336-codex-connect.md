---
kind: added
title: a ChatGPT plan connects as the Codex model service, in the browser or with codeaf connect
pr: 1336
surface: [chat, engine, docs]
invalidates:
  - "Codex was not available as a connected model service. A ChatGPT plan now signs in from `/connect` or `codeaf connect codex` and its account models appear as `codex/<slug>`."
  - "Every non-keyless model-service row opened key entry. The Codex row now says `browser` and uses the waiting card, copy affordance, and preferred-model move."
  - "A headless command refused to start with no OpenRouter key even when another service was connected. It now starts when any connected service holds its credential; a call to a service without one still fails when it is made."
  - "Connected-service documentation covered API-key services but not a ChatGPT plan's account list, limits, expiry, or unknown price. The Codex pages now state those boundaries and token-only accounting."
  - "On a WSL box without `xdg-open`, opening a link failed. A link now opens through `wslview` when it is present; Linux otherwise uses `xdg-open`, while macOS uses `open`."
  - "A headless run ended a Z.ai plan refusal with `your key was not accepted for this model` while the chat quoted the vendor's words. Headless and chat runs now end a payment refusal, including Codex's, with the same sentence: the service, that the account cannot pay, and what the vendor said; a spent fixed-price window says when it resets; an expired Codex sign-in says how to sign in again."
---

Codex uses the account's own visible model list and never exposes the persisted sentinel or rotating tokens to the catalog or screen. First run remains OpenRouter-only; device-code sign-in, OpenAI API-key sign-in, Codex base instructions, websockets, connector scopes, and the resident remain out of scope.
