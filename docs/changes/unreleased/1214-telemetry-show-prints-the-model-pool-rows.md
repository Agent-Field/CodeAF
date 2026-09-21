---
kind: changed
title: "The telemetry off switch quiets the Model Pool too, and `codeaf telemetry show` prints both streams"
pr: 1214
surface: [chat, engine, docs]
invalidates:
  - "`codeaf telemetry show` printed only the usage-count spool — two empty arrays on the day a person installs, when the notice had just told them to run it — so \"see exactly what leaves\" was untrue of the Model Pool rows and empty of meaning before the first run. It prints both streams now: every field with the value this machine would send, what each event adds, the never lists, then what is waiting, each under a line naming where it goes or why it is not sent."
  - "`CODEAF_TELEMETRY=off` stopped the usage counts and nothing else; the Model Pool went on posting model slugs and scores under `model_pool = on`, the default. Every rung of the telemetry off ladder — the variable, `DO_NOT_TRACK=1`, the project file, `codeaf telemetry off` — now caps the pool at `read`, over an explicit `on`, and `codeaf pool status` names the cap as `mode read · telemetry`."
  - "docs/TELEMETRY.md and the chat manual read as though the usage counts were the only thing sent to AgentField. The Model Pool is a second stream to codeaf.agentfield.ai, and both pages name it, its fields, its relay and its switches."
---
