---
kind: changed
title: The fuel meter asks an installed tariff first, and its table holds the ids this build ships
pr: 1093
surface: [engine]
invalidates:
  - "`orchestrate.PriceOf` answered from one package-level table and nothing else. An installed `PriceSource` is asked first and the table is the fallback behind it; `UsePrices` installs or removes one, and `CatalogPrices` adapts a per-token catalog reader into the per-million shape the meter uses."
  - "The fuel table now holds exactly the nine ids the shipped defaults and crew tables name: `google/gemini-2.5-flash`, `mistralai/mistral-nemo`, `deepseek/deepseek-v4-flash-0731`, `z-ai/glm-5.3-flash`, `z-ai/glm-5.3`, `moonshotai/kimi-k3`, `qwen/qwen3.8-max-0902`, `anthropic/claude-fable-5.1` and `anthropic/claude-opus-5`. `anthropic/claude-opus` and `openai/gpt-5` are gone, and both now meter at the unpriced rate rather than at a row of their own."
  - "An orchestrated run metered against the fuel table whenever a call reported no cost of its own. It meters against the catalog's published price when the session has one, and falls back to the table only for a model the catalog publishes nothing for."
---

The seam is a function value, so the meter never imports a catalog: the one
place an orchestrated run is built already holds a reader and installs it
there. A session with no reader installs nothing and leaves whatever is
installed alone, because the seam is shared by every run in the process.
