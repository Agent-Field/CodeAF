---
kind: added
title: a landed task is scored by a model outside the crew into the own sheet, the outbox and the judge seat
pr: 1093
surface: [chat, engine]
invalidates:
  - "The Model Pool's own sheet (`own.json` under the pool directory) could only hold scores a machine put there by hand. After a task lands in a chat-door conversation, a judge model outside the crew now scores each seat the work ran on, the scores are observed into that sheet, and the picker reads the new cells in the same process at once (`config.AutoOwnCells` repointed after every landing)."
  - "The outbox (`outbox.jsonl` beside the sheet) gained the same rows when `model_pool` is `on` and a submit address is set; `read` keeps them local and `off` asks no judge at all."
  - "The judge's provider calls are billed to the `judge` seat in the usage ledger, with the dollars from the model's published price when the provider sent no receipt of its own."
---

The engine grew the one seam it was missing: `session.Config.TaskLanded`, called
once per landed node on its own goroutine through `guard.Go`, carrying the
node's record as the landing left it plus the worker's model and the model its
checking pass ran on (`session.TaskLanding`). The chat door wires it to the
pool's judge (`cmd/codeaf/poolrecord.go`): pick a judge outside the crew
(`judge.Pick`), ask one question per held seat (`judge.Judge`), record what came
back (`record.Recorder`), and hand the new cells to the picker. A mode that
forbids reading builds no hook, and a nil hook costs the engine nothing.
