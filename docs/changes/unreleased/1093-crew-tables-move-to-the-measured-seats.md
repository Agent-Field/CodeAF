---
kind: changed
title: Six cells of the fixed crew tables moved to the measured seats
pr: 1093
surface: [chat, engine]
invalidates:
  - "The all family's frugal careful seat is `qwen/qwen3.8-max-0902` (was `google/gemini-3.8-flash`). Its balanced mastermind is `anthropic/claude-opus-5` (was `anthropic/claude-fable-5.1`). Its max row works on `z-ai/glm-5.3` (was `anthropic/claude-fable-5.1`), checks on `anthropic/claude-fable-5.1` (was `openai/gpt-6-astra`) and plans on `anthropic/claude-opus-5` (was `anthropic/claude-fable-5.1`). The open family's frugal row works on `z-ai/glm-5.3-flash` (was `deepseek/deepseek-v4-flash-0731`)."
  - "`config.DefaultMastermindModel` is `anthropic/claude-opus-5`, so the five shipped defaults are still exactly the default family's balanced row. An untouched profile plans on opus-5 and reads the crew as `balanced`; a profile that pinned the mastermind on the old default now reads `custom` until it pins again."
  - "Max now differs from balanced only in the worker seat, so an `auto` row on the worker can no longer tell the two presets apart — the stored-rows reading answers `balanced`, the default preset, and the worker's own id is what identifies max to the seam. The open family's frugal row has worker and careful on the same vendor, `glm-5.3-flash`, the one standing exception to the second-vendor law."
---

The cells move to the same measured plot the tables already name: expected bill
under each seat's call shape against published quality, nothing quoted. Max
spends on the seat that pays most of a task's bill; the careful and mastermind
seats settle at balanced and hold through max.
