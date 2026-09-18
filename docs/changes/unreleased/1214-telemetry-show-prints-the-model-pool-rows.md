---
kind: changed
title: "`codeaf telemetry show` prints the Model Pool's waiting rows beside the usage counts"
pr: 1214
surface: [chat, docs]
invalidates:
  - "`codeaf telemetry show` printed only the usage-count spool, so the notice's \"see exactly what leaves\" was untrue of the Model Pool rows going to codeaf.agentfield.ai. It prints both streams now, each under a line naming where it goes or why it is not sent."
  - "docs/TELEMETRY.md and the chat manual read as though the usage counts were the only thing sent to AgentField, and as though `CODEAF_TELEMETRY=off` stopped everything. The Model Pool is a second stream under `model_pool` / `CODEAF_MODEL_POOL`, and both pages say so."
---
