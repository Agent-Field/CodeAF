---
kind: added
title: OpenCode Go connects as a built-in plan provider
pr: 2298
surface: [chat, docs]
invalidates:
  - "The built-in provider list named OpenRouter, DeepSeek, GLM, Kimi, MiniMax, Qwen, Codex, Ollama and custom endpoints. OpenCode Go is now a built-in plan provider too."
---

OpenCode Go takes an `OPENCODE_API_KEY` through `/connect` or
`codeaf connect opencode-go`. The plan lists the models it serves and binds the
subscription as a plan with no metered overflow. Requests carry the conversation
lineage in `x-opencode-session`, which the service requires before it reads the
bearer. The crew can route the catalog vendors the plan serves.
