---
kind: fixed
title: a call's line in `aforge logs` shows its prompt tokens beside its completion tokens
pr: 364
surface: [engine, docs]
invalidates:
  - "`aforge logs` drew one unlabelled token segment — `466 tok`, the completion count — and never drew the prompt count at all, so a line said `$0.0003` without saying what it was for; a line now carries `4142 in` and `304 out` as two segments, with `(N thinking)` riding on `out` and `N cached` still its own segment beside them."
  - "The model-call record was never missing those counts. Issue #345 read a whole-body call's end row as carrying a cost and no tokens; the rows it was read from had been printed through a key filter that dropped them. A real `aforge do` on deepseek/deepseek-v4-flash writes `prompt_tokens`, `completion_tokens`, `stream` and `ttft_ms` on every end row, and it always did — the loss was in `cmd/aforge/logs.go`'s rendering and nowhere else."
  - "There is no non-streamed completion path to blame: every completion this adapter makes is asked for as a stream whether or not anybody is watching it, so an end row about an answer always carries `stream: true`. A row with no `stream` field is a row about an attempt that never became an answer."
---

The one figure a person opens this log for is what the money bought, and the prompt count is
also the only place a run's context size is written down. Both halves are named now, because
a single `tok` said neither which count it was nor how full the request had been.
