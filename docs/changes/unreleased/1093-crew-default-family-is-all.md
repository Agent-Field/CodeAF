---
kind: changed
title: The crew's default family is `all`, and both families' worker, careful and mastermind seats moved
pr: 1093
surface: [chat, engine]
invalidates:
  - "`models.crew.source` defaulted to `open`, so a profile that never answered the row resolved the open-weight table. The default is now `all`: `DefaultCrewSource = CrewSourceAll`, and a profile that never chose a family resolves the all family's balanced row — `google/gemini-2.5-flash`, `deepseek/deepseek-v4-flash-0731`, `z-ai/glm-5.3-flash`, `anthropic/claude-fable-5.1`, `anthropic/claude-fable-5.1`."
  - "The five shipped tier defaults were the open table's balanced row. `DefaultReflexModel` is now `google/gemini-2.5-flash`, `DefaultHighModel` and `DefaultMastermindModel` are `anthropic/claude-fable-5.1`; `DefaultLowModel` and `DefaultWorkerModel` are unchanged. The identity the crew row rests on still holds: the five defaults are exactly the DEFAULT family's balanced row."
  - "`config.CrewModels` and `config.CrewLine` answered the OPEN family. They answer the default family, which is now `all`; the open table is reached through `CrewModelsForSource(CrewSourceOpen, …)` and `CrewLineFor(CrewSourceOpen, …)`. A blank, misspelt or retired family word still folds to the default, which is `all` rather than `open`."
  - "A profile that applied a preset under an older build now reads `custom`, because the worker, careful and mastermind seats moved in both families. The crew word, the `/crew` highlight and the seat rung all say `custom` until the preset is applied again, which writes the new five."
  - "The open table's worker column is unchanged and its mastermind column is not: frugal now plans on `z-ai/glm-5.3-flash`, and balanced and max both plan on `z-ai/glm-5.3` rather than `moonshotai/kimi-k3`. The all table's worker holds `z-ai/glm-5.3-flash` through balanced and `openai/gpt-5.6-sol` and `anthropic/claude-opus-5` are in no preset of it any more."
  - "The `/crew` listing named the family whenever the row was not `open`. It names it whenever the row is not the default family, so an `open` listing carries the `family · open models` line and an `all` one does not."
---

Both tables are read off one plot, computed seat by seat on 2026-09-16: the
expected bill a seat's own call shape runs up, built from the catalog's
published prompt, completion and cache-read prices, against that seat's quality
from its published intelligence, coding and agentic indexes. The worker and the
careful seats are priced as long cached loops and the mastermind as one-shot
calls, which is why the columns no longer climb together in either family.

In the all family the worker stays on `glm-5.3-flash` through balanced because
the worker seat carries most of a task's tokens: a step there multiplies through
the whole bill, where a step on the careful or the mastermind seat is paid a
handful of times. The laws around the tables are untouched — the careful seat is
a different vendor from the worker in every preset of both families and still
sees images, and the reflex and small-work columns still never vary.
