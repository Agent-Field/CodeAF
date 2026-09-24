---
kind: fixed
title: Provider keys no longer reach a model's shell commands
pr: 1484
surface: [engine]
invalidates:
  - A bash command the model ran, foreground or background, inherited codeaf's whole process environment; a provider credential such as `OPENROUTER_API_KEY` was readable and printable from inside it. `JobShellEnv` now strips every provider key codeaf knows about — the OpenRouter/OpenAI variables, each vendored service's key variable, and any custom key variable a person named for a connected service — unless `CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL` is set in codeaf's own environment.
---
