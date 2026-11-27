---
kind: fixed
title: codeaf chat --once takes --events, --steer and --json, so a watched AgentField run starts
pr: 2999
surface: [chat, docs]
invalidates:
  - "`codeaf chat --once` on v0.8.2 and v0.8.3 refused `--json`, `--events` and `--steer` (\"codeaf chat has no --json flag\") and exited at once, so every watched run the AgentField Go SDK started — `codeaf chat --once … --json --events … --steer … --session …` — failed before it began and WorkAF showed no live transcript. From v0.8.4 the three flags are accepted with `--once`: `--events` copies the conversation's record line by line, `--steer` puts each appended line into the running turn, and `--json` prints one object `{ok, stop, answer, model, error, spend_usd, tokens}`."
  - "Without `--once` the three flags are refused with \"--events, --steer and --json go with --once\"."
---

The v0.8.x line carries the three flags and not the factory engine around them:
a question the model asks during such a run takes its own default, as any
headless run here does, rather than being relayed to a watcher. A steered run
prints its answer once (the doubled answer dev had is fixed in the same change).
